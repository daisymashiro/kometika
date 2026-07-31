package log

import (
	"context"
	"fmt"
	"os"
	"time"

	"cloud.google.com/go/firestore"
	firebase "firebase.google.com/go/v4"
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

	opt := option.WithCredentialsJSON([]byte(credJSON))
	app, err := firebase.NewApp(context.Background(), nil, opt)
	if err != nil {
		return err
	}

	client, err := app.Firestore(context.Background())
	if err != nil {
		return err
	}
	firestoreClient = client
	return nil
}

// FirestoreZapHook akan dipanggil oleh Zap setiap kali ada log baru
func FirestoreZapHook(entry zapcore.Entry) error {
	// Opsional: Filter hanya level Warn, Error, dan Fatal agar kuota Firestore aman
	if entry.Level < zapcore.WarnLevel {
		return nil
	}

	if firestoreClient == nil {
		return nil
	}

	// Gunakan goroutine agar pengiriman log tidak memblokir performa bot utama
	go func(e zapcore.Entry) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		_, _, err := firestoreClient.Collection("bot_logs").Add(ctx, map[string]interface{}{
			"level":   e.Level.String(),
			"time":    e.Time,
			"logger":  e.LoggerName,
			"message": e.Message,
			"caller":  e.Caller.String(),
			// "stack": e.Stack, // Buka komen ini jika ingin melihat stack trace
		})
		if err != nil {
			fmt.Printf("Gagal mengirim log ke Firebase: %v\n", err)
		}
	}(entry)

	return nil
}
