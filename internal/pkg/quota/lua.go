package quota
import "github.com/redis/go-redis/v9"
var multiBucketScript=redis.NewScript(`
local t=redis.call('TIME'); local now=tonumber(t[1])*1000+math.floor(tonumber(t[2])/1000)
local states={}; local denied=0; local retry=0
for i,key in ipairs(KEYS) do
 local j=(i-1)*4; local cap=tonumber(ARGV[j+1]); local rate=tonumber(ARGV[j+2]); local cost=tonumber(ARGV[j+3]); local scope=ARGV[j+4]
 local old=redis.call('HMGET',key,'tokens','ts'); local tokens=tonumber(old[1]); local ts=tonumber(old[2])
 if tokens==nil then tokens=cap; ts=now end
 tokens=math.min(cap,tokens+math.max(0,now-ts)*rate/1000)
 states[i]={tokens=tokens,cap=cap,rate=rate,cost=cost,scope=scope}
 if denied==0 and tokens<cost then denied=i; if rate>0 then retry=math.ceil((cost-tokens)*1000/rate) else retry=2147483647 end end
end
if denied>0 then return {0,states[denied].scope,math.floor(states[denied].tokens),retry} end
local remaining=2147483647
for i,key in ipairs(KEYS) do local s=states[i]; local n=s.tokens-s.cost; redis.call('HSET',key,'tokens',n,'ts',now); local ttl=60; if s.rate>0 then ttl=math.max(1,math.ceil(s.cap/s.rate*2)) end; redis.call('EXPIRE',key,ttl); if n<remaining then remaining=n end end
return {1,'',math.floor(remaining),0}
`)
