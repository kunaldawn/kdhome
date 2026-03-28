#!/bin/sh
# Fix ownership of mounted data directory, then drop to appuser
chown -R appuser:appuser /app/data 2>/dev/null || true
exec su-exec appuser ./server
