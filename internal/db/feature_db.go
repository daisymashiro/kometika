package db

import (
	"database/sql"
	"log"

	_ "modernc.org/sqlite"
)

var DB *sql.DB

func InitDB(dbPath string) {
	var err error

	DB, err = sql.Open("sqlite", dbPath)
	if err != nil {
		log.Fatalf("Gagal membuka database: %v", err)
	}

	DB.SetMaxOpenConns(1)

	_, _ = DB.Exec("PRAGMA journal_mode=WAL;")
	_, _ = DB.Exec("PRAGMA busy_timeout=5000;")

	createTableQuery := `
	CREATE TABLE IF NOT EXISTS feature_toggles (
		feature_name TEXT PRIMARY KEY,
		is_enabled BOOLEAN NOT NULL
	);`

	_, err = DB.Exec(createTableQuery)
	if err != nil {
		log.Fatalf("Gagal membuat tabel feature_toggles: %v", err)
	}
}

func SetFeatureStatus(feature string, isEnabled bool) error {
	query := `
	INSERT INTO feature_toggles (feature_name, is_enabled) 
	VALUES (?, ?) 
	ON CONFLICT(feature_name) DO UPDATE SET is_enabled = excluded.is_enabled;`

	_, err := DB.Exec(query, feature, isEnabled)
	return err
}

func GetAllFeatures() (map[string]bool, error) {
	features := make(map[string]bool)
	rows, err := DB.Query("SELECT feature_name, is_enabled FROM feature_toggles")
	if err != nil {
		return features, err
	}
	defer rows.Close()

	for rows.Next() {
		var name string
		var isEnabled bool
		if err := rows.Scan(&name, &isEnabled); err == nil {
			features[name] = isEnabled
		}
	}
	return features, nil
}
