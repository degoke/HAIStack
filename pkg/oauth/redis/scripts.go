package redis

import goredis "github.com/redis/go-redis/v9"

const consumeBoundJSONScript = `
local function normalize(s)
  if type(s) ~= 'string' then
    return ''
  end
  s = string.gsub(s, '^%s+', '')
  s = string.gsub(s, '%s+$', '')
  s = string.gsub(s, '/+$', '')
  return s
end

local payload = redis.call('GET', KEYS[1])
if not payload then
  return ''
end
local ok, obj = pcall(cjson.decode, payload)
if not ok or type(obj) ~= 'table' then
  return ''
end
if normalize(obj['issuer']) ~= ARGV[1] then
  return ''
end
redis.call('DEL', KEYS[1])
return payload
`

const consumeBoundRefreshScript = `
local function normalize(s)
  if type(s) ~= 'string' then
    return ''
  end
  s = string.gsub(s, '^%s+', '')
  s = string.gsub(s, '%s+$', '')
  s = string.gsub(s, '/+$', '')
  return s
end

local payload = redis.call('HGET', KEYS[1], 'payload')
if not payload then
  return ''
end
local ok, obj = pcall(cjson.decode, payload)
if not ok or type(obj) ~= 'table' then
  return ''
end
if normalize(obj['issuer']) ~= ARGV[1] then
  return ''
end
redis.call('DEL', KEYS[1])
return payload
`

var (
	// consumeBoundJSONValueScript deletes a JSON string key only when issuer matches.
	consumeBoundJSONValueScript = goredis.NewScript(consumeBoundJSONScript)

	// consumeBoundRefreshScript deletes a refresh hash only when issuer matches.
	consumeBoundRefreshTokenScript = goredis.NewScript(consumeBoundRefreshScript)

	saveRefreshTokenScript = goredis.NewScript(`
redis.call('HSET', KEYS[1], 'clientId', ARGV[1], 'payload', ARGV[2])
redis.call('EXPIRE', KEYS[1], tonumber(ARGV[3]))
return 1
`)

	deleteRefreshTokenForClientScript = goredis.NewScript(`
if redis.call('HGET', KEYS[1], 'clientId') == ARGV[1] then
  return redis.call('DEL', KEYS[1])
end
return 0
`)
)
