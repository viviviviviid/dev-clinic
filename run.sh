#!/bin/bash
# coding-tutor homeserver control script
# Usage: ./run.sh [start|stop|restart|status|logs]

NAME="coding-tutor"
BIN="./bin/coding-tutor-server"

case "$1" in
  start)
    pm2 start $BIN --name $NAME
    pm2 save
    ;;
  stop)
    pm2 stop $NAME
    ;;
  restart)
    pm2 restart $NAME
    ;;
  status)
    pm2 show $NAME
    ;;
  logs)
    pm2 logs $NAME --lines ${2:-50}
    ;;
  deploy)
    echo "Building..."
    make build-homeserver
    echo "Restarting..."
    pm2 restart $NAME 2>/dev/null || pm2 start $BIN --name $NAME
    pm2 save
    echo "Done."
    ;;
  *)
    echo "Usage: $0 {start|stop|restart|status|logs [lines]|deploy}"
    exit 1
    ;;
esac
