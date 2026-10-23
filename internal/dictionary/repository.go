// Package dictionary 提供基于老版 Pieb.db 游戏数据字典的高性能只读查询仓储。
//
// @author Ateng
// @since 2026-10-08
package dictionary

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	_ "modernc.org/sqlite"
)

var (
	// ErrNotFound 表示指定的字典数据记录未找到。
	ErrNotFound = errors.New("dictionary: entity not found")
)

// Repository 定义游戏数据字典查询仓储接口。
type Repository interface {
	GetItem(id int) (*Item, error)
	GetNPC(id int) (*NPC, error)
	GetRoleType(id int) (*RoleType, error)
	GetMission(id int) (*Mission, error)
	GetHighestMission(sweepElite bool) (*Mission, error)
	GetHighestMissionByLevel(level int, sweepElite bool) (*Mission, error)
	GetHighestMissionByType(missionType int) (*Mission, error)
	GetHighestMissionByTypeAndLevel(level int, missionType int) (*Mission, error)
	SearchItems(keyword string) ([]*Item, error)
	Close() error
}

type sqliteRepository struct {
	db *sql.DB
	mu sync.RWMutex
}

// ResolveDatabasePath 智能探测 Pieb.db 数据库物理路径。
func ResolveDatabasePath(customPath string) (string, error) {
	if customPath != "" {
		if _, err := os.Stat(customPath); err == nil {
			return customPath, nil
		}
		return "", fmt.Errorf("dictionary: 指定的数据库文件不存在: %s", customPath)
	}

	candidates := []string{
		filepath.Join("data", "Pieb.db"),
		filepath.Join("..", "data", "Pieb.db"),
		filepath.Join("..", "..", "data", "Pieb.db"),
		filepath.Join("..", "..", "..", "data", "Pieb.db"),
		`D:\Software\疯玩神仙道\Pieb.db`,
	}

	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}

	return "", errors.New("dictionary: 未找到 Pieb.db 数据库文件，请确认已放置在 data/ 目录")
}

// NewRepository 创建并初始化 SQLite 数据字典仓储。
func NewRepository(dbPath string) (Repository, error) {
	actualPath, err := ResolveDatabasePath(dbPath)
	if err != nil {
		return nil, err
	}

	// 1. 构建只读挂载连接串
	dsn := fmt.Sprintf("file:%s?mode=ro&cache=shared", filepath.ToSlash(actualPath))
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("dictionary: 打开 SQLite 失败: %w", err)
	}

	// 2. 验证连接连通性
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("dictionary: 连接 SQLite 失败: %w", err)
	}

	return &sqliteRepository{db: db}, nil
}

func (r *sqliteRepository) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.db != nil {
		err := r.db.Close()
		r.db = nil
		return err
	}
	return nil
}

func (r *sqliteRepository) GetItem(id int) (*Item, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	query := `SELECT id, sxdname, type_id, description, quality, require_level, price, ingot FROM item WHERE id = ? LIMIT 1`
	row := r.db.QueryRow(query, id)

	var it Item
	var name sql.NullString
	var desc sql.NullString
	err := row.Scan(&it.ID, &name, &it.TypeID, &desc, &it.Quality, &it.RequireLevel, &it.Price, &it.Ingot)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("dictionary: 查询道具失败: %w", err)
	}
	it.Name = strings.TrimSpace(name.String)
	it.Description = desc.String
	return &it, nil
}

func (r *sqliteRepository) GetNPC(id int) (*NPC, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	query := `SELECT id, townid, npcid, npcname, npcx, npcy FROM NPCposition WHERE id = ? OR npcid = ? LIMIT 1`
	row := r.db.QueryRow(query, id, id)

	var n NPC
	var name sql.NullString
	err := row.Scan(&n.ID, &n.TownID, &n.NPCID, &name, &n.CoordX, &n.CoordY)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("dictionary: 查询 NPC 失败: %w", err)
	}
	n.Name = strings.TrimSpace(name.String)
	return &n, nil
}

func (r *sqliteRepository) GetRoleType(id int) (*RoleType, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	query := `SELECT id, sxdname, JobId, Type, Fame, StuntId, Gender FROM roletype WHERE id = ? LIMIT 1`
	row := r.db.QueryRow(query, id)

	var role RoleType
	var name sql.NullString
	err := row.Scan(&role.ID, &name, &role.JobID, &role.Type, &role.Fame, &role.StuntID, &role.Gender)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("dictionary: 查询角色类型失败: %w", err)
	}
	role.Name = strings.TrimSpace(name.String)
	return &role, nil
}

func (r *sqliteRepository) SearchItems(keyword string) ([]*Item, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	trimmed := strings.TrimSpace(keyword)
	if trimmed == "" {
		return []*Item{}, nil
	}

	query := `SELECT id, sxdname, type_id, description, quality, require_level, price, ingot FROM item WHERE sxdname LIKE ? LIMIT 50`
	rows, err := r.db.Query(query, "%"+trimmed+"%")
	if err != nil {
		return nil, fmt.Errorf("dictionary: 搜索道具失败: %w", err)
	}
	defer rows.Close()

	items := make([]*Item, 0)
	for rows.Next() {
		var it Item
		var name sql.NullString
		var desc sql.NullString
		if err := rows.Scan(&it.ID, &name, &it.TypeID, &desc, &it.Quality, &it.RequireLevel, &it.Price, &it.Ingot); err != nil {
			return nil, fmt.Errorf("dictionary: 扫描道具数据失败: %w", err)
		}
		it.Name = strings.TrimSpace(name.String)
		it.Description = desc.String
		items = append(items, &it)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("dictionary: 读取道具结果集失败: %w", err)
	}

	return items, nil
}

func (r *sqliteRepository) GetMission(id int) (*Mission, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	query := `SELECT MissionsId, SectionId, MissionLock, MissionPower, map, mapkey, MissionName, MissionType, isBossMission, monsters FROM Missions WHERE MissionsId = ? LIMIT 1`
	row := r.db.QueryRow(query, id)

	var m Mission
	var name sql.NullString
	var monsters sql.NullString
	var isBoss int
	err := row.Scan(&m.ID, &m.SectionID, &m.MissionLock, &m.Power, &m.MapID, &m.MapKey, &name, &m.Type, &isBoss, &monsters)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("dictionary: 查询副本关卡失败: %w", err)
	}
	m.Name = strings.TrimSpace(name.String)
	m.Monsters = monsters.String
	m.IsBoss = isBoss == 1
	return &m, nil
}

func (r *sqliteRepository) GetHighestMission(sweepElite bool) (*Mission, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	query := `SELECT MissionsId, SectionId, MissionLock, MissionPower, map, mapkey, MissionName, MissionType, isBossMission, monsters FROM Missions WHERE (? OR MissionType = 0) ORDER BY MissionsId DESC LIMIT 1`
	row := r.db.QueryRow(query, sweepElite)

	var m Mission
	var name sql.NullString
	var monsters sql.NullString
	var isBoss int
	err := row.Scan(&m.ID, &m.SectionID, &m.MissionLock, &m.Power, &m.MapID, &m.MapKey, &name, &m.Type, &isBoss, &monsters)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("dictionary: 查询最高等级副本关卡失败: %w", err)
	}
	m.Name = strings.TrimSpace(name.String)
	m.Monsters = monsters.String
	m.IsBoss = isBoss == 1
	return &m, nil
}

func (r *sqliteRepository) GetHighestMissionByLevel(level int, sweepElite bool) (*Mission, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if level <= 0 {
		level = 1
	}
	maxLock := level*100 + 1000
	var questLock sql.NullInt64
	qRow := r.db.QueryRow("SELECT max(Lock) FROM Quests WHERE Level <= ? AND Lock < 90000 AND Type = 1 AND Level >= 10", level)
	if qErr := qRow.Scan(&questLock); qErr == nil && questLock.Valid && questLock.Int64 > 0 {
		maxLock = int(questLock.Int64)
	}
	if level <= 1 {
		maxLock = 1020 // 确保初级新手角色至少可以锁定小竹林新手关
	}

	query := `SELECT MissionsId, SectionId, MissionLock, MissionPower, map, mapkey, MissionName, MissionType, isBossMission, monsters 
              FROM Missions 
              WHERE MissionLock <= ? AND (? OR MissionType = 0) 
              ORDER BY MissionLock DESC, MissionsId DESC 
              LIMIT 1`
	row := r.db.QueryRow(query, maxLock, sweepElite)

	var m Mission
	var name sql.NullString
	var monsters sql.NullString
	var isBoss int
	err := row.Scan(&m.ID, &m.SectionID, &m.MissionLock, &m.Power, &m.MapID, &m.MapKey, &name, &m.Type, &isBoss, &monsters)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// 若未查到，降级返回全局最低第一关
			fallbackQuery := `SELECT MissionsId, SectionId, MissionLock, MissionPower, map, mapkey, MissionName, MissionType, isBossMission, monsters 
                              FROM Missions 
                              WHERE (? OR MissionType = 0) 
                              ORDER BY MissionLock ASC, MissionsId ASC 
                              LIMIT 1`
			fbRow := r.db.QueryRow(fallbackQuery, sweepElite)
			if fbErr := fbRow.Scan(&m.ID, &m.SectionID, &m.MissionLock, &m.Power, &m.MapID, &m.MapKey, &name, &m.Type, &isBoss, &monsters); fbErr != nil {
				return nil, ErrNotFound
			}
			m.Name = strings.TrimSpace(name.String)
			m.Monsters = monsters.String
			m.IsBoss = isBoss == 1
			return &m, nil
		}
		return nil, fmt.Errorf("dictionary: 按等级查询最高副本关卡失败: %w", err)
	}
	m.Name = strings.TrimSpace(name.String)
	m.Monsters = monsters.String
	m.IsBoss = isBoss == 1
	return &m, nil
}

func (r *sqliteRepository) GetHighestMissionByType(missionType int) (*Mission, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	query := `SELECT MissionsId, SectionId, MissionLock, MissionPower, map, mapkey, MissionName, MissionType, isBossMission, monsters 
              FROM Missions 
              WHERE MissionType = ? 
              ORDER BY MissionsId DESC 
              LIMIT 1`
	row := r.db.QueryRow(query, missionType)

	var m Mission
	var name sql.NullString
	var monsters sql.NullString
	var isBoss int
	err := row.Scan(&m.ID, &m.SectionID, &m.MissionLock, &m.Power, &m.MapID, &m.MapKey, &name, &m.Type, &isBoss, &monsters)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("dictionary: 查询指定类型最高副本关卡失败: %w", err)
	}
	m.Name = strings.TrimSpace(name.String)
	m.Monsters = monsters.String
	m.IsBoss = isBoss == 1
	return &m, nil
}

func (r *sqliteRepository) GetHighestMissionByTypeAndLevel(level int, missionType int) (*Mission, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if level <= 0 {
		level = 1
	}
	maxLock := level*100 + 1000
	var questLock sql.NullInt64
	qRow := r.db.QueryRow("SELECT max(Lock) FROM Quests WHERE Level <= ? AND Lock < 90000 AND Type = 1 AND Level >= 10", level)
	if qErr := qRow.Scan(&questLock); qErr == nil && questLock.Valid && questLock.Int64 > 0 {
		maxLock = int(questLock.Int64)
	}
	if level <= 1 {
		maxLock = 1020
	}

	query := `SELECT MissionsId, SectionId, MissionLock, MissionPower, map, mapkey, MissionName, MissionType, isBossMission, monsters 
              FROM Missions 
              WHERE MissionLock <= ? AND MissionType = ? 
              ORDER BY MissionLock DESC, MissionsId DESC 
              LIMIT 1`
	row := r.db.QueryRow(query, maxLock, missionType)

	var m Mission
	var name sql.NullString
	var monsters sql.NullString
	var isBoss int
	err := row.Scan(&m.ID, &m.SectionID, &m.MissionLock, &m.Power, &m.MapID, &m.MapKey, &name, &m.Type, &isBoss, &monsters)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// 若未查到，降级返回该类型全局最低第一关
			fallbackQuery := `SELECT MissionsId, SectionId, MissionLock, MissionPower, map, mapkey, MissionName, MissionType, isBossMission, monsters 
                              FROM Missions 
                              WHERE MissionType = ? 
                              ORDER BY MissionLock ASC, MissionsId ASC 
                              LIMIT 1`
			fbRow := r.db.QueryRow(fallbackQuery, missionType)
			if fbErr := fbRow.Scan(&m.ID, &m.SectionID, &m.MissionLock, &m.Power, &m.MapID, &m.MapKey, &name, &m.Type, &isBoss, &monsters); fbErr != nil {
				return nil, ErrNotFound
			}
			m.Name = strings.TrimSpace(name.String)
			m.Monsters = monsters.String
			m.IsBoss = isBoss == 1
			return &m, nil
		}
		return nil, fmt.Errorf("dictionary: 按等级与类型查询最高副本关卡失败: %w", err)
	}
	m.Name = strings.TrimSpace(name.String)
	m.Monsters = monsters.String
	m.IsBoss = isBoss == 1
	return &m, nil
}
