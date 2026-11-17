package redis

const (
	LuaCheckEnableAndWriteCache = `
	local disable_key = KEYS[1];
	local disable_flag = redis.call("get",disable_key);
	if disable_flag then
	    return 0;
	end
	local key = KEYS[2];
	local value = ARGV[1];
	local cache_expire_seconds = tonumber(ARGV[2]);
	redis.call("set",key,value,"ex",cache_expire_seconds);
	return 1;
`
)
