---
name: weather
description: Query city weather. Call when the user asks about weather, temperature, or rain.
apis:
  - name: query_weather
    description: Query the current weather of the given city
    endpoint: ${WEATHER_API_BASE_URL}/api/weather
    method: GET
    parameters:
      city:
        type: string
        description: City name (in Chinese, e.g. 北京)
        required: true
      unit:
        type: string
        description: Temperature unit
        enum: ["celsius", "fahrenheit"]
        default: "celsius"
      user_id:
        type: string
        required: true
        injected: true
---

# weather skill usage

To query the weather, call the query_weather tool first, passing the city name (e.g. 北京).
