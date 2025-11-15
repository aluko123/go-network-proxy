
-- KEY[1]: Redis key (e.g. "proxy:ratelimit:<ip>")
-- ARGV[1] = rate (max requests per second)
-- ARGV[3] = current timestamp in milliseconds
-- ARGV[2] = time window in milliseconds

local key = KEYS[1]
local limit = tonumber(ARGV[1])
local window = tonumber(ARGV[2])
local current_time = tonumber(ARGV[3])


-- Calculate window start time
local window_start = current_time - window

-- Step 1: Count requests in the current window
local request_count = redis.call('ZCOUNT', key, window_start, current_time)

--Step2: Check if the request count exceeds the limit
if request_count < limit then
    -- Allow: Add curr timestamp to ZSET
    redis.call('ZADD', key, current_time, current_time)

    -- Cleanup old entries
    redis.call('ZREMRANGEBYSCORE', key, '-inf', window_start)

    -- Set expiration for the key
    redis.call('EXPIRE', key, math.ceil(window / 1000))

    return 1 -- Allowed
else
    return 0 -- Rate limit exceeded
end
