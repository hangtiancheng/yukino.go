package task

const SQLGetMinuteTaskCnt = `SELECT LEFT(run_timer,16) AS minute,count(*) AS cnt FROM task WHERE run_timer >= ? AND run_timer < ? GROUP BY LEFT(run_timer,16) `
