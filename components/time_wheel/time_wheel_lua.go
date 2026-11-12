package time_wheel

const (
	LuaAddTasks = `
	   local zsetKey = KEYS[1]
	   local indexKey = KEYS[2]
	   local score = ARGV[1]
	   local task = ARGV[2]
	   local taskKey = ARGV[3]
	   local oldTask = redis.call('hget',indexKey,taskKey)
	   if oldTask then
	       redis.call('zrem',zsetKey,oldTask)
	   end
	   redis.call('hset',indexKey,taskKey,task)
	   return redis.call('zadd',zsetKey,score,task)
	`

	LuaDeleteTask = `
	   local zsetKey = KEYS[1]
	   local indexKey = KEYS[2]
	   local taskKey = ARGV[1]
	   local task = redis.call('hget',indexKey,taskKey)
	   if not task then
	       return 0
       end
	   redis.call('zrem',zsetKey,task)
	   return redis.call('hdel',indexKey,taskKey)
	`

	LuaZrangeTasks = `
	   local zsetKey = KEYS[1]
	   local indexKey = KEYS[2]
	   local maxScore = ARGV[1]
	   local targets = redis.call('zrangebyscore',zsetKey,'-inf',maxScore,'limit',0,1000)
	   local decoded = {}
	   for i, taskJSON in ipairs(targets) do
	       local ok, task = pcall(cjson.decode,taskJSON)
	       if not ok or type(task) ~= 'table' or type(task.key) ~= 'string' or task.key == '' then
	           return redis.error_reply('invalid time wheel task payload')
	       end
	       decoded[i] = task
	   end
	   for i, taskJSON in ipairs(targets) do
	       local task = decoded[i]
	       if redis.call('hget',indexKey,task.key) == taskJSON then
	           redis.call('hdel',indexKey,task.key)
	       end
	       redis.call('zrem',zsetKey,taskJSON)
	   end
	   return targets
	`
)
