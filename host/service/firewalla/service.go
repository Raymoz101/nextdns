// Package firewalla implements the Firewalla package init system.

package firewalla

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/nextdns/nextdns/host/service"
	"github.com/nextdns/nextdns/host/service/internal"
)

type Service struct {
	service.Config
	service.ConfigFileStorer
	Path string
}

const confDir = "/home/pi/.firewalla/config"
const initDir = "/home/pi/.firewalla/config/post_main.d"

func New(c service.Config) (Service, error) {
	if _, err := os.Stat("/etc/firewalla_release"); err != nil {
		return Service{}, service.ErrNotSupported
	}
	return Service{
		Config:           c,
		ConfigFileStorer: service.ConfigFileStorer{File: filepath.Join(confDir, c.Name+".conf")},
		Path:             filepath.Join(initDir, c.Name+".sh"),
	}, nil
}

func (s Service) Install() error {
	_ = os.MkdirAll(initDir, 0755)
	if err := internal.CreateWithTemplate(s.Path, tmpl, 0755, s.Config); err != nil {
		return err
	}
	return nil
}

func (s Service) Uninstall() error {
	if err := os.Remove(s.Path); err != nil {
		if os.IsNotExist(err) {
			return service.ErrNoInstalled
		}
		return err
	}
	return nil
}

func (s Service) Status() (service.Status, error) {
	out, err := internal.RunOutput(s.Path, "status")
	switch {
	case strings.HasPrefix(out, "Running"):
		return service.StatusRunning, nil
	case strings.HasPrefix(out, "Stopped"):
		return service.StatusStopped, nil
	default:
		if err != nil {
			return service.StatusUnknown, err
		}
		return service.StatusNotInstalled, nil
	}
}

func (s Service) Start() error {
	return internal.Run(s.Path, "start")
}

func (s Service) Stop() error {
	return internal.Run(s.Path, "stop")
}

func (s Service) Restart() error {
	return internal.Run(s.Path, "restart")
}

var tmpl = `#!/bin/bash

cmd="{{.Executable}}{{range .Arguments}} {{.}}{{end}}"

name={{.Name}}

# The daemon runs as a transient systemd unit instead of a detached
# background process tracked by a pidfile:
# - at boot this script is executed from firewalla.service (Type=oneshot,
#   KillMode=control-group); a plain background process is killed by
#   systemd's cgroup cleanup as soon as that unit's start-up run exits,
# - Restart=on-failure recovers daemon crashes,
# - the pidfile lived on persistent storage, so after a reboot a stale
#   PID could match an unrelated process, making start report "Already
#   started" without starting anything and stop kill the wrong process.

is_running() {
	systemctl is-active --quiet "$name"
}

action=$1
if [ -z "$action" ]; then
	action=start
fi

case "$action" in
	start)
		if is_running; then
			echo "Already started"
		else
			echo "Starting $name"
			# A previously failed transient unit would block systemd-run.
			sudo systemctl reset-failed "$name" 2>/dev/null
			# Stop any daemon left over from the previous pidfile-based
			# version of this script so it cannot hold the listen address.
			sudo pkill -x "$name" 2>/dev/null
			sudo systemd-run --quiet --unit="$name" \
				--property=Environment={{.RunModeEnv}}=1 \
				--property=Restart=on-failure \
				--property=RestartSec=5 \
				$cmd
			sleep 1
			if ! is_running; then
				echo "Unable to start"
				exit 1
			fi
		fi
	;;
	stop)
		if is_running; then
			echo "Stopping $name"
			sudo systemctl stop "$name"
			if is_running; then
				echo "Not stopped; may still be shutting down or shutdown may have failed"
				exit 1
			fi
			echo "Stopped"
		else
			echo "Not running"
		fi
	;;
	restart)
		if is_running; then
			sudo systemctl restart "$name"
		else
			$0 start
		fi
	;;
	status)
		if is_running; then
			echo "Running"
		else
			echo "Stopped"
			exit 1
		fi
	;;
	*)
	echo "Usage: $0 {start|stop|restart|status}"
	exit 1
	;;
esac
exit 0
`
