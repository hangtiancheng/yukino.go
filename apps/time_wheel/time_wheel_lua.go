// Copyright (c) 2026 hangtiancheng
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package time_wheel

const (
	// LuaAddTasks: when adding a task, if a delete marker exists for the key, remove it first.
	// The task is routed to a shard based on its minute-level timestamp.
	LuaAddTasks = `
	   local zsetKey = KEYS[1]
	   local deleteSetKey = KEYS[2]
	   local score = ARGV[1]
	   local task = ARGV[2]
	   local taskKey = ARGV[3]
	   redis.call('srem',deleteSetKey,taskKey)
	   return redis.call('zadd',zsetKey,score,task)
	`

	// LuaDeleteTask: mark the task key as deleted.
	LuaDeleteTask = `
	   local deleteSetKey = KEYS[1]
	   local taskKey = ARGV[1]
	   redis.call('sadd',deleteSetKey,taskKey)
	   local scnt = redis.call('scard',deleteSetKey)
	   if (tonumber(scnt) == 1)
	   then
	       redis.call('expire',deleteSetKey,120)
       end
	   return scnt
	`

	// LuaZrangeTasks: fetch all tasks whose delete markers are absent via zrange.
	LuaZrangeTasks = `
	   local zsetKey = KEYS[1]
	   local deleteSetKey = KEYS[2]
	   local score1 = ARGV[1]
	   local score2 = ARGV[2]
	   local deleteSet = redis.call('smembers',deleteSetKey)
	   local targets = redis.call('zrange',zsetKey,score1,score2,'byscore')
	   redis.call('zremrangebyscore',zsetKey,score1,score2)
	   local reply = {}
	   reply[1] = deleteSet
	   for i, v in ipairs(targets) do
	       reply[#reply+1]=v
	   end
       return reply
	`
)
