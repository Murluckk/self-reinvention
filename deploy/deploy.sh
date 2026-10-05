#!/usr/bin/env bash
# Выкатка на VPS одной командой: тесты → сборка → копия базы → замена
# бинарника → проверка запуска → автоматический откат, если бот не поднялся.
#
#   DEPLOY_HOST=root@1.2.3.4 make deploy
#
# Адрес сервера в репозиторий не попадает: его задают переменной или кладут
# в незакоммиченный .deploy.env (DEPLOY_HOST=root@…), который читает Makefile.
set -euo pipefail

HOST="${DEPLOY_HOST:?задай DEPLOY_HOST=user@host или положи его в .deploy.env}"
cd "$(dirname "$0")/.."
VERSION="$(git describe --always --dirty)"

if [[ "$VERSION" == *-dirty && "${ALLOW_DIRTY:-}" != 1 ]]; then
	echo "✗ есть незакоммиченные изменения; закоммить или ALLOW_DIRTY=1" >&2
	exit 1
fi

echo "→ проверки"
test -z "$(gofmt -l .)" || { gofmt -l .; echo "✗ gofmt" >&2; exit 1; }
go vet ./...
go test ./...

case "$(ssh "$HOST" uname -m)" in
	x86_64) GOARCH=amd64 ;;
	aarch64) GOARCH=arm64 ;;
	*) echo "✗ неизвестная архитектура сервера" >&2; exit 1 ;;
esac

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
echo "→ сборка $VERSION linux/$GOARCH"
CGO_ENABLED=0 GOOS=linux GOARCH="$GOARCH" go build -trimpath \
	-ldflags="-s -w -X main.version=$VERSION" -o "$TMP/tracker-bot" ./cmd/bot

echo "→ копирую на $HOST"
ssh "$HOST" 'rm -rf /tmp/tracker-deploy && mkdir -p /tmp/tracker-deploy'
scp -q "$TMP/tracker-bot" deploy/tracker-bot.service deploy/tracker-alert.service deploy/alert.sh \
	"$HOST:/tmp/tracker-deploy/"

ssh "$HOST" bash -s -- "$VERSION" <<'REMOTE'
set -euo pipefail
VERSION="$1"
SRC=/tmp/tracker-deploy
DIR=/opt/tracker
STAMP="$(date +%Y%m%d-%H%M%S)"
SNAP="$DIR/data/predeploy/$STAMP"

echo "→ systemd-юниты"
install -m 0755 "$SRC/alert.sh" "$DIR/alert.sh"
install -m 0644 "$SRC/tracker-alert.service" /etc/systemd/system/tracker-alert.service
if ! cmp -s "$SRC/tracker-bot.service" /etc/systemd/system/tracker-bot.service; then
	cp -a /etc/systemd/system/tracker-bot.service "/etc/systemd/system/tracker-bot.service.bak-$STAMP"
	diff -u "/etc/systemd/system/tracker-bot.service.bak-$STAMP" "$SRC/tracker-bot.service" || true
	install -m 0644 "$SRC/tracker-bot.service" /etc/systemd/system/tracker-bot.service
fi
systemctl daemon-reload

echo "→ останавливаю бота и снимаю копию базы в $SNAP"
systemctl stop tracker-bot
install -d -o tracker -g tracker -m 0750 "$SNAP"
cp -a "$DIR"/data/tracker.db* "$SNAP"/
cp -a "$DIR/tracker-bot" "$DIR/tracker-bot.prev"
install -o tracker -g tracker -m 0755 "$SRC/tracker-bot" "$DIR/tracker-bot"
# Храним пять последних снимков перед выкаткой.
ls -1dt "$DIR"/data/predeploy/*/ 2>/dev/null | tail -n +6 | xargs -r rm -rf

STARTED="$(date '+%Y-%m-%d %H:%M:%S')"
systemctl start tracker-bot
for _ in $(seq 1 20); do
	sleep 1
	if journalctl -u tracker-bot --since "$STARTED" -o cat | grep -q 'msg=подключился'; then
		echo "✓ $VERSION запущен"
		journalctl -u tracker-bot --since "$STARTED" -o cat | tail -n 8
		rm -rf "$SRC"
		exit 0
	fi
done

echo "✗ бот не поднялся за 20 секунд — откатываю" >&2
journalctl -u tracker-bot --since "$STARTED" -o cat | tail -n 20 >&2
systemctl stop tracker-bot
cp -a "$DIR/tracker-bot.prev" "$DIR/tracker-bot"
cp -a "$SNAP"/tracker.db* "$DIR/data/"
systemctl start tracker-bot
exit 1
REMOTE
