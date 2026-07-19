package repo

import (
	"context"
	"database/sql"
)

// LoadAreaTriggerTeleportDestinations imports immutable game data. The
// registry does not retain the database connection after startup.
func LoadAreaTriggerTeleportDestinations(ctx context.Context, db *sql.DB) (map[uint32]uint32, error) {
	rows, err := db.QueryContext(ctx, "SELECT ID, target_map FROM areatrigger_teleport")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	destinations := make(map[uint32]uint32)
	for rows.Next() {
		var triggerID, mapID uint32
		if err := rows.Scan(&triggerID, &mapID); err != nil {
			return nil, err
		}
		destinations[triggerID] = mapID
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return destinations, nil
}

// LoadInstanceMaps returns the authoritative dungeon and raid map catalog.
func LoadInstanceMaps(ctx context.Context, db *sql.DB) ([]uint32, error) {
	rows, err := db.QueryContext(ctx, "SELECT DISTINCT map FROM instance_template ORDER BY map")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var maps []uint32
	for rows.Next() {
		var mapID uint32
		if err := rows.Scan(&mapID); err != nil {
			return nil, err
		}
		maps = append(maps, mapID)
	}
	return maps, rows.Err()
}
