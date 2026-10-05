#!/bin/sh
# Пишет владельцу в Telegram, что tracker-bot упал и systemd перестал его
# перезапускать. Сам бот в этот момент молчит, поэтому нужен внешний сигнал.
set -eu
ENV=/etc/tracker/bot.env
# EnvironmentFile — не shell-скрипт, поэтому не source, а точечное чтение.
TOKEN="$(sed -n 's/^BOT_TOKEN=//p' "$ENV" | head -n1)"
OWNER="$(sed -n 's/^OWNER_ID=//p' "$ENV" | head -n1)"
LOGS="$(journalctl -u tracker-bot -n 8 --no-pager -o cat 2>/dev/null || true)"
TEXT="🚨 tracker-bot на $(hostname) упал и не поднимается.

$LOGS"
curl -fsS -m 20 \
	--data-urlencode "chat_id=$OWNER" \
	--data-urlencode "text=$TEXT" \
	"https://api.telegram.org/bot$TOKEN/sendMessage" >/dev/null
