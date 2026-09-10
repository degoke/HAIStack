package redis

import goredis "github.com/redis/go-redis/v9"

var (
	consumeRefreshTokenScript = goredis.NewScript(`
local payload = redis.call('HGET', KEYS[1], 'payload')
if not payload then
  return ''
end
redis.call('DEL', KEYS[1])
return payload
`)

	deleteRefreshTokenForClientScript = goredis.NewScript(`
if redis.call('HGET', KEYS[1], 'clientId') == ARGV[1] then
  return redis.call('DEL', KEYS[1])
end
return 0
`)
)
