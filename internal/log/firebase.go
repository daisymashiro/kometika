package log

import (
	"context"
	"fmt"
	"os"
	"time"

	"cloud.google.com/go/firestore"
	"go.uber.org/zap/zapcore"
	"google.golang.org/api/option"
)

var firestoreClient *firestore.Client

// InitFirebase menginisialisasi koneksi ke Firestore menggunakan JSON dari .env
func InitFirebase() error {
	credJSON := os.Getenv("FIREBASE_CRED_JSON")
	if credJSON == "" {
		return fmt.Errorf("FIREBASE_CRED_JSON belum diset di .env")
	}

	projectID := os.Getenv("FIREBASE_PROJECT_ID")
	if projectID == "" {
		return fmt.Errorf("FIREBASE_PROJECT_ID belum diset di .env")
	}

	dbID := os.Getenv("FIRESTORE_DATABASE_ID")
	if dbID == "" {
		dbID = "kometika" // fallback
	}

	opt := option.WithCredentialsJSON([]byte(credJSON))

	// 🔥 Perbaikan di sini: gunakan NewClientWithDatabase, bukan option.WithDatabaseID
	client, err := firestore.NewClientWithDatabase(context.Background(), projectID, dbID, opt)
	if err != nil {
		return err
	}
	firestoreClient = client
	return nil
}

// FirestoreZapHook akan dipanggil oleh Zap setiap kali ada log baru
func FirestoreZapHook(entry zapcore.Entry) error {
	if entry.Level < zapcore.WarnLevel {
		return nil
	}
	if firestoreClient == nil {
		return nil
	}

	go func(e zapcore.Entry) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		_, _, err := firestoreClient.Collection("bot_logs").Add(ctx, map[string]interface{}{
			"level":   e.Level.String(),
			"time":    e.Time,
			"logger":  e.LoggerName,
			"message": e.Message,
			"caller":  e.Caller.String(),
		})
		if err != nil {
			fmt.Printf("Gagal mengirim log ke Firebase: %v\n", err)
		}
	}(entry)

	return nil
}

