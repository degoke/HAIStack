package redis

import goredis "github.com/redis/go-redis/v9"

var (
	// consumeJSONValueScript atomically reads and deletes a string value.
	consumeJSONValueScript = goredis.NewScript(`
local payload = redis.call('GET', KEYS[1])
if not payload then
  return ''
end
redis.call('DEL', KEYS[1])
return payload
`)

	saveRefreshTokenScript = goredis.NewScript(`
redis.call('HSET', KEYS[1], 'clientId', ARGV[1], 'payload', ARGV[2])
redis.call('EXPIRE', KEYS[1], tonumber(ARGV[3]))
return 1
`)

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
