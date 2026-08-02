package services

import (
	"testing"

	"github.com/daodao97/xgo/xdb"
)

// 批量 alias 占用校验：过期别名放行、大小写按 NOCASE 命中、自有别名放行、空列表快速返回
func TestCheckNamesNotOccupiedByAlias(t *testing.T) {
	setupRenameTestEnv(t)
	db, err := xdb.DB("default")
	if err != nil {
		t.Fatalf("获取数据库失败: %v", err)
	}

	seed := []struct {
		alias   string
		owner   int64
		expires string
	}{
		{"Taken", 100, "datetime('now', '+1 hour')"},
		{"Expired", 100, "datetime('now', '-1 hour')"},
		{"MINE", 7, "datetime('now', '+1 hour')"},
	}
	for _, s := range seed {
		if _, err := db.Exec(
			`INSERT INTO provider_alias (platform, provider_id, alias_name, canonical_name, expires_at)
			 VALUES ('claude', ?, ?, ?, `+s.expires+`)`, s.owner, s.alias, s.alias); err != nil {
			t.Fatalf("预置 alias 失败: %v", err)
		}
	}

	if err := checkNamesNotOccupiedByAlias("claude", nil); err != nil {
		t.Errorf("空列表应放行: %v", err)
	}
	if err := checkNamesNotOccupiedByAlias("claude", []Provider{{ID: 1, Name: "Fresh"}}); err != nil {
		t.Errorf("未占用名字应放行: %v", err)
	}
	if err := checkNamesNotOccupiedByAlias("claude", []Provider{{ID: 1, Name: "Expired"}}); err != nil {
		t.Errorf("过期别名应放行: %v", err)
	}
	if err := checkNamesNotOccupiedByAlias("claude", []Provider{{ID: 7, Name: "mine"}}); err != nil {
		t.Errorf("自有别名(大小写不同)应放行: %v", err)
	}
	if err := checkNamesNotOccupiedByAlias("claude", []Provider{{ID: 1, Name: "taken"}}); err == nil {
		t.Error("他人活动别名(大小写不同)应拒绝")
	}
	if err := checkNamesNotOccupiedByAlias("claude", []Provider{{ID: 1, Name: "Taken"}}); err == nil {
		t.Error("他人活动别名应拒绝")
	}
	// 混合列表：一个冲突即整体拒绝
	if err := checkNamesNotOccupiedByAlias("claude", []Provider{
		{ID: 1, Name: "Fresh"}, {ID: 2, Name: "TAKEN"},
	}); err == nil {
		t.Error("混合列表包含冲突应拒绝")
	}
}
