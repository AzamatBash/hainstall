package store

import "database/sql"

// MapRemnaNodeAlertNotes returns saved explanations keyed by panelID + NUL + remna UUID.
func (s *Store) MapRemnaNodeAlertNotes() (map[string]string, error) {
	rows, err := s.db.Query(`
SELECT panel_id, remna_uuid, alert_note
FROM remna_node_catalog
WHERE alert_note != ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var panelID, uuid, note string
		if err := rows.Scan(&panelID, &uuid, &note); err != nil {
			return nil, err
		}
		out[panelID+"\x00"+uuid] = note
	}
	return out, rows.Err()
}

// SetRemnaNodeAlertNote stores why a node's warning is happening. Empty clears it.
func (s *Store) SetRemnaNodeAlertNote(panelID, remnaUUID, note string) error {
	res, err := s.db.Exec(`
UPDATE remna_node_catalog SET alert_note = ?
WHERE panel_id = ? AND remna_uuid = ?`, note, panelID, remnaUUID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}
