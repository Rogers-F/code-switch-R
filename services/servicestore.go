package services

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

type SuiStore struct {
	db *sql.DB
}

func getSafeDBPath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	appDir := filepath.Join(configDir, "SuiNest")
	err = os.MkdirAll(appDir, 0755)
	if err != nil {
		return "", err
	}
	return filepath.Join(appDir, "suidemo.db"), nil
}
func NewSuiStore() (*SuiStore, error) {
	dbPath, err := getSafeDBPath()
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}

	// 创建表
	_, err = db.Exec(`
	CREATE TABLE IF NOT EXISTS hotkeys (
	    id INTEGER PRIMARY KEY AUTOINCREMENT,
	    keycode INTEGER NOT NULL,
	    modifiers INTEGER NOT NULL,
	    description TEXT,
		target TEXT
	);
	`)
	if err != nil {
		db.Close()
		return nil, err
	}
	// 检查是否已存在数据
	row := db.QueryRow(`SELECT COUNT(*) FROM hotkeys`)
	var count int
	err = row.Scan(&count)
	if err != nil {
		db.Close()
		return nil, err
	}

	if count == 0 {
		_, err = db.Exec(`
			INSERT INTO hotkeys (keycode, modifiers, description, target)
			VALUES
				(34, 768, 'testdemo', '1'),
				(46, 768, 'testdemo2', '2');
		`)
		if err != nil {
			db.Close()
			return nil, err
		}
	}

	return &SuiStore{db: db}, nil
}

func (cs *SuiStore) Close() {
	cs.db.Close()
}

func (cs *SuiStore) Start() error {
	// 这里可以初始化数据库或其它启动逻辑
	return nil
}

func (cs *SuiStore) Stop() error {
	cs.Close()
	return nil
}

type Hotkey struct {
	ID        int    `json:"id"`        // 热键ID
	KeyCode   uint32 `json:"keycode"`   // 键码
	Modifiers uint32 `json:"modifiers"` // 修饰键
}

// 快捷键修改
func (cs *SuiStore) UpHotkey(id int, key int, modifier int) error {

	_, err := cs.db.Exec(`
        UPDATE hotkeys 
        SET keycode = ?, modifiers = ? 
        WHERE id = ?
    `, key, modifier, id)
	fmt.Println("🌂🌂🌂🌂🌂🌂", key, modifier)
	return err
}

func (cs *SuiStore) GetHotkeys() ([]Hotkey, error) {
	rows, err := cs.db.Query("SELECT id, keycode, modifiers FROM hotkeys")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var hotkeys []Hotkey
	for rows.Next() {
		var hk Hotkey
		if err := rows.Scan(&hk.ID, &hk.KeyCode, &hk.Modifiers); err != nil {
			return nil, err
		}
		hotkeys = append(hotkeys, hk)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return hotkeys, nil
}
