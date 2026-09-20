package bot

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const backupRetention = 30 * 24 * time.Hour

// SendBackup создаёт консистентную копию общей базы, хранит её локально и
// отправляет владельцу в Telegram. Это off-site копия без отдельного облака:
// потеря диска VPS не уничтожит историю.
func (b *Bot) SendBackup(ctx context.Context) error {
	name := "tracker-" + b.cfg.Today() + ".db"
	path := filepath.Join(b.cfg.BackupDir, name)
	if err := b.st.Backup(path); err != nil {
		return err
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	caption := fmt.Sprintf("🔐 Ежедневный бэкап трекера за %s · пользователей: %d", b.cfg.Today(), len(b.cfg.Users))
	if err := b.tg.SendDocument(ctx, b.cfg.OwnerID, name, content, caption); err != nil {
		return err
	}
	b.pruneBackups()
	return nil
}

func (b *Bot) pruneBackups() {
	entries, err := os.ReadDir(b.cfg.BackupDir)
	if err != nil {
		b.log.Warn("чтение каталога бэкапов", "err", err)
		return
	}
	cutoff := time.Now().Add(-backupRetention)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "tracker-") || !strings.HasSuffix(entry.Name(), ".db") {
			continue
		}
		info, err := entry.Info()
		if err == nil && info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(b.cfg.BackupDir, entry.Name()))
		}
	}
}
