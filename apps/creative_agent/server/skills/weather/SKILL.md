---
name: weather
description: 查询城市天气。当用户询问天气、气温、降雨时调用。
apis:
  - name: query_weather
    description: 查询指定城市的当前天气
    endpoint: ${WEATHER_API_BASE_URL}/api/weather
    method: GET
    parameters:
      city:
        type: string
        description: 城市名（中文）
        required: true
      unit:
        type: string
        description: 温度单位
        enum: ["celsius", "fahrenheit"]
        default: "celsius"
      user_id:
        type: string
        required: true
        injected: true
---

# weather skill 使用说明

查询天气时先调用 query_weather 工具，传入城市名（如「北京」）。
