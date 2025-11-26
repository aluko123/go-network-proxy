-- Leaky Bucket Rate Limiter
-- KEY[1]: Redis key (e.g. "proxy:ratelimit:<ip>")
-- ARGV[1]: bucket capacity (burst size)
-- ARGV[2]: leak rate (tokens per second)
-- ARGV[3]: current timestamp in milliseconds

local key = KEYS[1]
local capacity = tonumber(ARGV[1])
local leak_rate = tonumber(ARGV[2])
local now = tonumber(ARGV[3])

-- Get current bucket state: [level, last_update]
local bucket = redis.call('HMGET', key, 'level', 'last_update')
local level = tonumber(bucket[1]) or 0
local last_update = tonumber(bucket[2]) or now

-- Calculate leaked amount since last update
local elapsed_ms = now - last_update
local leaked = (elapsed_ms / 1000) * leak_rate

-- Update level: drain leaked tokens, but don't go below 0
level = math.max(0, level - leaked)

-- Try to add one token (request)
if level < capacity then
    -- Bucket has room, allow request
    level = level + 1
    redis.call('HSET', key, 'level', level, 'last_update', now)
    redis.call('EXPIRE', key, math.ceil(capacity / leak_rate) + 1)
    return 1 -- Allowed
else
    -- Bucket full, reject request
    redis.call('HSET', key, 'last_update', now)
    return 0 -- Rate limited
end
