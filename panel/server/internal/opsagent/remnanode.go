package opsagent

import (
	"fmt"
	"strings"

	"github.com/azabash/hapanel/panel/internal/provision"
)

type remnanodeDetect struct {
	Found     bool
	Container string
	Network   string
	Host8443  bool
	Warn      string
}

func detectRemnanodeScript() string {
	return `set +e
CID=$(docker ps --format '{{.Names}}' 2>/dev/null | grep -Ei '^remnanode$|remnanode' | head -1)
if [ -z "$CID" ]; then
  CID=$(docker ps --format '{{.Names}} {{.Image}}' 2>/dev/null | awk 'BEGIN{IGNORECASE=1} /remnawave\/node|remnanode/ {print $1; exit}')
fi
if [ -z "$CID" ]; then
  echo "REMNA_FOUND=0"
  exit 0
fi
NET=$(docker inspect "$CID" --format '{{range $k,$v := .NetworkSettings.Networks}}{{$k}} {{end}}' 2>/dev/null | awk '{print $1}')
PORTMAP=$(docker port "$CID" 8443 2>/dev/null | head -1)
echo "REMNA_FOUND=1"
echo "REMNA_CONTAINER=$CID"
echo "REMNA_NETWORK=$NET"
if [ -n "$PORTMAP" ]; then
  echo "REMNA_HOST_8443=1"
  echo "REMNA_PORTMAP=$PORTMAP"
else
  echo "REMNA_HOST_8443=0"
fi
exit 0
`
}

func parseRemnanodeDetect(out string) remnanodeDetect {
	var d remnanodeDetect
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch k {
		case "REMNA_FOUND":
			d.Found = v == "1"
		case "REMNA_CONTAINER":
			d.Container = v
		case "REMNA_NETWORK":
			d.Network = v
		case "REMNA_HOST_8443":
			d.Host8443 = v == "1"
		case "REMNA_PORTMAP":
			if strings.TrimSpace(v) != "" {
				d.Warn = "remnanode публикует :8443 на хост (" + v + ") — снимите ports у remnanode, HAProxy займёт :8443"
			}
		}
	}
	return d
}

func patchBundleForRemnanode(bundle *provision.Bundle, container, network string) {
	if bundle == nil || container == "" || network == "" {
		return
	}
	compose := bundle.Files["docker-compose.yml"]
	compose = strings.Replace(compose,
		"    networks:\n      - hapnet\n",
		"    networks:\n      - hapnet\n      - remnanode_ext\n",
		1,
	)
	compose = strings.Replace(compose,
		"networks:\n  hapnet:\n    driver: bridge\n",
		fmt.Sprintf("networks:\n  hapnet:\n    driver: bridge\n  remnanode_ext:\n    external: true\n    name: %q\n", network),
		1,
	)
	bundle.Files["docker-compose.yml"] = compose

	app := strings.TrimRight(bundle.Files["haproxy/backends.d/app.cfg"], "\n")
	app += fmt.Sprintf("\n    server remnanode_local %s:8443 weight 100\n", container)
	bundle.Files["haproxy/backends.d/app.cfg"] = app
}

func clearConflictsScript(keepRemnanode bool) string {
	if keepRemnanode {
		return `set +e
echo "=== keep remnanode: nginx only ==="
systemctl stop nginx 2>/dev/null
systemctl disable nginx 2>/dev/null
service nginx stop 2>/dev/null
if command -v nginx >/dev/null 2>&1; then
  nginx -s stop 2>/dev/null
fi
if command -v fuser >/dev/null 2>&1; then
  fuser -k 80/tcp 2>/dev/null
fi
ss -lptn 'sport = :80 or sport = :8443' 2>/dev/null || netstat -lptn 2>/dev/null | grep -E ':80|:8443' || true
echo "conflicts cleanup done (remnanode untouched)"
exit 0
`
	}
	return clearConflictsScriptFull()
}

func clearConflictsScriptFull() string {
	return `set +e
echo "=== stop nginx ==="
systemctl stop nginx 2>/dev/null
systemctl disable nginx 2>/dev/null
service nginx stop 2>/dev/null
if command -v nginx >/dev/null 2>&1; then
  nginx -s stop 2>/dev/null
fi

echo "=== stop remnanode compose dirs ==="
for d in /opt/remnanode /root/remnanode /opt/remnawave /opt/remnawave/node; do
  if [ -f "$d/docker-compose.yml" ] || [ -f "$d/compose.yml" ] || [ -f "$d/docker-compose.yaml" ]; then
    echo "down $d"
    (cd "$d" && docker compose down --remove-orphans) 2>/dev/null
    (cd "$d" && docker-compose down --remove-orphans) 2>/dev/null
  fi
done

echo "=== stop remnanode/remnawave containers ==="
docker ps -aq --filter name=remnanode 2>/dev/null | xargs -r docker stop
docker ps -aq --filter name=remnanode 2>/dev/null | xargs -r docker rm
docker ps -aq --filter ancestor=remnawave/node 2>/dev/null | xargs -r docker stop
docker ps -aq --filter ancestor=remnawave/node 2>/dev/null | xargs -r docker rm
docker ps --format '{{.ID}} {{.Names}} {{.Image}}' 2>/dev/null | awk 'BEGIN{IGNORECASE=1} /remna|rw-core|xray/ {print $1}' | xargs -r docker stop

echo "=== free 80/8443 if still busy ==="
if command -v fuser >/dev/null 2>&1; then
  fuser -k 80/tcp 2>/dev/null
  fuser -k 8443/tcp 2>/dev/null
fi
ss -lptn 'sport = :80 or sport = :8443' 2>/dev/null || netstat -lptn 2>/dev/null | grep -E ':80|:8443' || true
echo "conflicts cleanup done"
exit 0
`
}
