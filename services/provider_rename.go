package services

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/daodao97/xgo/xdb"
)

// aliasTTL 定义 rename 后旧名保留时长,必须 > in-flight 请求上限(32h)并留 buffer。
const aliasTTL = 48 * time.Hour

// RenameProvider 改名 provider:事务更新 DB 中按 name 存储的历史数据,
// 写入 48h alias 兜底 in-flight 请求,最后原子替换配置文件。
//
// 校验规则:
//   - newName 非空且 trim 后与 oldName 不等
//   - 同 kind 下不存在其它 provider 用同名(当前 snapshot)
//   - 48h 内 alias 表未占用该 newName
//   - 该 provider_id 在 48h 内未 rename 过(禁止链式)
func (ps *ProviderService) RenameProvider(kind string, id int64, newName string) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	newName = strings.TrimSpace(newName)
	if newName == "" {
		return fmt.Errorf("新名字不能为空")
	}

	platform, err := resolvePlatform(kind)
	if err != nil {
		return err
	}

	// 清理过期 alias(MVP:不起后台 job,借 rename 顺手 GC)
	if err := cleanupExpiredAliases(); err != nil {
		return fmt.Errorf("清理过期 alias 失败: %w", err)
	}

	// 加载当前配置(原样读,不触发迁移保存)
	providers, err := ps.loadProvidersRaw(kind)
	if err != nil {
		return fmt.Errorf("读取配置失败: %w", err)
	}

	var target *Provider
	for i := range providers {
		if providers[i].ID == id {
			target = &providers[i]
			break
		}
	}
	if target == nil {
		return fmt.Errorf("未找到 id=%d 的 provider", id)
	}
	oldName := target.Name

	if strings.EqualFold(strings.TrimSpace(oldName), newName) {
		return fmt.Errorf("新名字与旧名字相同")
	}

	// 校验当前 snapshot 内同 kind 不冲突
	for i := range providers {
		if providers[i].ID == id {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(providers[i].Name), newName) {
			return fmt.Errorf("同 kind 下已存在名为 %q 的 provider", newName)
		}
	}

	// 校验 alias 表内是否被占用 + 该 provider_id 48h 内是否已 rename
	if err := checkAliasConstraints(platform, id, newName); err != nil {
		return err
	}

	// 备份原 JSON bytes(用于 DB commit 失败时补偿)
	originalBytes, err := serializeProviders(providers)
	if err != nil {
		return fmt.Errorf("序列化原配置失败: %w", err)
	}

	// 更新内存中的 provider.Name,序列化新配置
	target.Name = newName
	newBytes, err := serializeProviders(providers)
	if err != nil {
		return fmt.Errorf("序列化新配置失败: %w", err)
	}
	path, err := providerFilePath(kind)
	if err != nil {
		return err
	}

	// 1) 先原子替换文件
	if err := atomicWriteFile(path, newBytes, 0o644); err != nil {
		return fmt.Errorf("写入配置文件失败: %w", err)
	}
	// 后续任一 DB 步骤失败都需要把 JSON 还原到 rename 前;回滚本身失败要合并到返回错误,避免 split-brain 被静默。
	rollbackFile := func(primary error) error {
		if rbErr := atomicWriteFile(path, originalBytes, 0o644); rbErr != nil {
			log.Printf("[RenameProvider] CRITICAL 回滚配置文件失败 path=%s primary=%v rollback=%v", path, primary, rbErr)
			return fmt.Errorf("%w; 配置文件回滚失败: %v", primary, rbErr)
		}
		return primary
	}

	// 2) 开启 DB 事务更新历史数据 + 写 alias
	db, err := xdb.DB("default")
	if err != nil {
		return rollbackFile(fmt.Errorf("获取数据库连接失败: %w", err))
	}

	tx, err := db.Begin()
	if err != nil {
		return rollbackFile(fmt.Errorf("开启事务失败: %w", err))
	}
	if err := doRenameTx(tx, platform, id, oldName, newName); err != nil {
		_ = tx.Rollback()
		return rollbackFile(fmt.Errorf("更新历史数据失败: %w", err))
	}
	if err := tx.Commit(); err != nil {
		return rollbackFile(fmt.Errorf("提交事务失败: %w", err))
	}

	return nil
}

// doRenameTx 在 tx 内完成 DB 侧所有改动:
// request_log.provider / provider_blacklist.provider_name / health_check_history + 写 alias。
func doRenameTx(tx *sql.Tx, platform string, providerID int64, oldName, newName string) error {
	if _, err := tx.Exec(
		`UPDATE request_log SET provider = ? WHERE platform = ? AND provider = ?`,
		newName, platform, oldName,
	); err != nil {
		return fmt.Errorf("更新 request_log 失败: %w", err)
	}

	if _, err := tx.Exec(
		`UPDATE provider_blacklist SET provider_name = ? WHERE platform = ? AND provider_name = ?`,
		newName, platform, oldName,
	); err != nil {
		return fmt.Errorf("更新 provider_blacklist 失败: %w", err)
	}

	if _, err := tx.Exec(
		`UPDATE health_check_history SET provider_name = ? WHERE platform = ? AND provider_id = ?`,
		newName, platform, providerID,
	); err != nil {
		return fmt.Errorf("更新 health_check_history 失败: %w", err)
	}

	expiresAt := time.Now().Add(aliasTTL).UTC().Format("2006-01-02 15:04:05")
	if _, err := tx.Exec(
		`INSERT INTO provider_alias (platform, provider_id, alias_name, canonical_name, expires_at)
		 VALUES (?, ?, ?, ?, ?)`,
		platform, providerID, oldName, newName, expiresAt,
	); err != nil {
		return fmt.Errorf("写入 alias 失败: %w", err)
	}

	return nil
}

// checkAliasConstraints 校验 alias 表层面的约束:
//   - newName 未被 48h 内其它 alias 占用
//   - 该 provider_id 48h 内没有产生过 alias(禁止链式 rename)
func checkAliasConstraints(platform string, providerID int64, newName string) error {
	db, err := xdb.DB("default")
	if err != nil {
		return fmt.Errorf("获取数据库连接失败: %w", err)
	}

	var occupied int
	err = db.QueryRow(
		`SELECT COUNT(*) FROM provider_alias
		 WHERE platform = ? AND alias_name = ? AND expires_at > CURRENT_TIMESTAMP`,
		platform, newName,
	).Scan(&occupied)
	if err != nil {
		return fmt.Errorf("查询 alias 占用失败: %w", err)
	}
	if occupied > 0 {
		return fmt.Errorf("新名字 %q 在 48h 内被历史别名占用,无法使用", newName)
	}

	var chained int
	err = db.QueryRow(
		`SELECT COUNT(*) FROM provider_alias
		 WHERE platform = ? AND provider_id = ? AND expires_at > CURRENT_TIMESTAMP`,
		platform, providerID,
	).Scan(&chained)
	if err != nil {
		return fmt.Errorf("查询链式 rename 失败: %w", err)
	}
	if chained > 0 {
		return fmt.Errorf("该 provider 48h 内已改过名,请等 alias 过期后再操作")
	}

	return nil
}

// checkNameNotOccupiedByAlias 校验 `name` 未被其它 provider 的 48h 活动 alias 占用。
// 用于 SaveProviders 新建/更新时阻止"复用旧名新建 provider 污染历史"。
// providerID 为当前被保存的 provider id;如果 alias 的 provider_id 等于它本身,则不算冲突
// (意味着是该 provider 自己的老别名,canonical_name 仍指向它自己,不会误归并)。
func checkNameNotOccupiedByAlias(platform string, providerID int64, name string) error {
	if name == "" {
		return nil
	}
	db, err := xdb.DB("default")
	if err != nil {
		return fmt.Errorf("获取数据库连接失败: %w", err)
	}
	var owner int64
	err = db.QueryRow(
		`SELECT provider_id FROM provider_alias
		 WHERE platform = ? AND alias_name = ? AND expires_at > CURRENT_TIMESTAMP
		 LIMIT 1`,
		platform, name,
	).Scan(&owner)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return fmt.Errorf("查询 alias 占用失败: %w", err)
	}
	if owner != providerID {
		log.Printf("[Provider] 名字 %q 被其他 provider(id=%d)的 48h 活动别名占用,拒绝保存", name, owner)
		return fmt.Errorf("名字 %q 被其他供应商的历史别名暂时占用(48h 内),请换个名字或等待过期", name)
	}
	return nil
}

// checkNamesNotOccupiedByAlias 是 checkNameNotOccupiedByAlias 的批量版：
// 单条 IN 查询取回全部活动 alias 后在内存比对，供 SaveProviders 持锁路径使用
// （逐个查询在保存 N 个供应商时是 N 次串行 DB 往返）。
// alias_name 列为 COLLATE NOCASE（SQLite 内建 NOCASE 仅折叠 ASCII A-Z），
// 内存比对必须用同口径的 ASCII 折叠，不能用 Unicode 语义的 EqualFold/ToLower
func checkNamesNotOccupiedByAlias(platform string, providers []Provider) error {
	if len(providers) == 0 {
		return nil
	}
	db, err := xdb.DB("default")
	if err != nil {
		return fmt.Errorf("获取数据库连接失败: %w", err)
	}
	placeholders := make([]string, 0, len(providers))
	args := make([]interface{}, 0, len(providers)+1)
	args = append(args, platform)
	idByFoldedName := make(map[string]int64, len(providers))
	for _, p := range providers {
		if p.Name == "" {
			continue
		}
		placeholders = append(placeholders, "?")
		args = append(args, p.Name)
		idByFoldedName[asciiFold(p.Name)] = p.ID
	}
	if len(placeholders) == 0 {
		return nil
	}
	rows, err := db.Query(
		`SELECT alias_name, provider_id FROM provider_alias
		 WHERE platform = ? AND expires_at > CURRENT_TIMESTAMP
		   AND alias_name IN (`+strings.Join(placeholders, ",")+`)`,
		args...,
	)
	if err != nil {
		return fmt.Errorf("查询 alias 占用失败: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var aliasName string
		var owner int64
		if err := rows.Scan(&aliasName, &owner); err != nil {
			return fmt.Errorf("查询 alias 占用失败: %w", err)
		}
		id, ok := idByFoldedName[asciiFold(aliasName)]
		if !ok {
			continue
		}
		// alias 的 provider_id 等于该 provider 自身时不算冲突（自己的老别名）
		if owner != id {
			log.Printf("[Provider] 名字 %q 被其他 provider(id=%d)的 48h 活动别名占用,拒绝保存", aliasName, owner)
			return fmt.Errorf("名字 %q 被其他供应商的历史别名暂时占用(48h 内),请换个名字或等待过期", aliasName)
		}
	}
	return rows.Err()
}

// asciiFold 仅折叠 ASCII A-Z 到小写，逐字节，与 SQLite 内建 NOCASE 完全同口径
func asciiFold(s string) string {
	b := []byte(s)
	changed := false
	for i := 0; i < len(b); i++ {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 'a' - 'A'
			changed = true
		}
	}
	if !changed {
		return s
	}
	return string(b)
}

// cleanupExpiredAliases 删除已过期的 alias 记录。
func cleanupExpiredAliases() error {
	db, err := xdb.DB("default")
	if err != nil {
		return err
	}
	_, err = db.Exec(`DELETE FROM provider_alias WHERE expires_at <= CURRENT_TIMESTAMP`)
	return err
}

// ResolveProviderAlias 将旧名翻译为当前 canonical name(未过期),找不到返回原名。
// 只做 1 跳查询,由 RenameProvider 的链式拒绝约束保证不会出现多层 alias。
func ResolveProviderAlias(platform, name string) string {
	if name == "" {
		return name
	}
	db, err := xdb.DB("default")
	if err != nil {
		return name
	}
	var canonical string
	err = db.QueryRow(
		`SELECT canonical_name FROM provider_alias
		 WHERE platform = ? AND alias_name = ? AND expires_at > CURRENT_TIMESTAMP
		 LIMIT 1`,
		platform, name,
	).Scan(&canonical)
	if err != nil || canonical == "" {
		return name
	}
	return canonical
}

// resolvePlatform 把 kind 归一到 DB 使用的 platform 值(与 request_log/blacklist 一致)。
func resolvePlatform(kind string) (string, error) {
	switch strings.ToLower(kind) {
	case "claude", "claude-code", "claude_code":
		return "claude", nil
	case "codex":
		return "codex", nil
	default:
		if strings.HasPrefix(kind, "custom:") {
			return kind, nil
		}
		return "", fmt.Errorf("不支持的 provider kind: %s", kind)
	}
}

// serializeProviders 按 saveProvidersLocked 相同的 MarshalIndent 格式输出。
func serializeProviders(providers []Provider) ([]byte, error) {
	return json.MarshalIndent(providerEnvelope{Providers: providers}, "", "  ")
}
