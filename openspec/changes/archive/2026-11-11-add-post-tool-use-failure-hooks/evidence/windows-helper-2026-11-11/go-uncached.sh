#!/bin/sh
# 全量测试显式重跑，其他质量门原样转发。
if [ "$1" = test ]; then
  shift
  exec "$EASYCODE_REGRESSION_GO" test -count=1 "$@"
fi
exec "$EASYCODE_REGRESSION_GO" "$@"
