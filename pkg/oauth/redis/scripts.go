package redis

import goredis "github.com/redis/go-redis/v9"

const luaBoundHelpers = `
local function normalize(s)
  if type(s) ~= 'string' then
    return ''
  end
  s = string.gsub(s, '^%s+', '')
  s = string.gsub(s, '%s+$', '')
  s = string.gsub(s, '/+$', '')
  return s
end

local function expired(obj, nowns)
  local exp = tonumber(obj['exp'])
  if not exp then
    return true
  end
  return exp <= tonumber(nowns)
end

local function decode(payload)
  if not payload then
    return nil
  end
  local ok, obj = pcall(cjson.decode, payload)
  if not ok or type(obj) ~= 'table' then
    return nil
  end
  return obj
end

local function usable(payload, issuer, nowns)
  local obj = decode(payload)
  if not obj then
    return false
  end
  if normalize(obj['issuer']) ~= issuer then
    return false
  end
  if expired(obj, nowns) then
    return false
  end
  return true
end
`

var (
	// peekBoundJSONValueScript returns a JSON string key when issuer matches and exp is live.
	peekBoundJSONValueScript = goredis.NewScript(luaBoundHelpers + `
local payload = redis.call('GET', KEYS[1])
if not usable(payload, ARGV[1], ARGV[2]) then
  return ''
end
return payload
`)

	// deleteBoundJSONIfMatchScript deletes a JSON string key only when the payload is unchanged.
	deleteBoundJSONIfMatchScript = goredis.NewScript(`
local payload = redis.call('GET', KEYS[1])
if payload ~= ARGV[1] then
  return 0
end
redis.call('DEL', KEYS[1])
return 1
`)

	// peekBoundRefreshTokenScript returns a refresh hash payload when issuer matches and exp is live.
	peekBoundRefreshTokenScript = goredis.NewScript(luaBoundHelpers + `
local payload = redis.call('HGET', KEYS[1], 'payload')
if not usable(payload, ARGV[1], ARGV[2]) then
  return ''
end
return payload
`)

	// deleteBoundRefreshIfMatchScript deletes a refresh hash only when the payload is unchanged.
	deleteBoundRefreshIfMatchScript = goredis.NewScript(`
local payload = redis.call('HGET', KEYS[1], 'payload')
if payload ~= ARGV[1] then
  return 0
end
redis.call('DEL', KEYS[1])
return 1
`)

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
