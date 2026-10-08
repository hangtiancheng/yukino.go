package llm

import "testing"

func TestSQLPolicy(t *testing.T) {
	for _, sql := range []string{"SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE()", "SELECT '<script>set; drop</script>'", "SELECT 'can\\'t; alter'", "SELECT `title` FROM risk_records;", "WITH rows AS (SELECT id FROM risk_records) SELECT * FROM rows", "SHOW TABLES", "UPDATE risk_records SET content = 'safe' WHERE id = 1"} {
		if _, err := validateSQL(sql, true); err != nil {
			t.Errorf("valid SQL rejected: %s: %v", sql, err)
		}
	}
	for _, sql := range []string{"SELECT 1; DELETE FROM risk_records", "SELECT 1 /*!50000 INTO OUTFILE '/tmp/x' */", "SELECT LOAD_FILE('/etc/passwd')", "SELECT SLEEP(60)", "EXPLAIN DELETE FROM risk_records", "WITH rows AS (SELECT 1) DELETE FROM risk_records", "UPDATE `executions` SET status = 'pending'", "UPDATE taskflow_outbox SET published_at = NULL", "SELECT * FROM mysql.user", "SELECT 'unterminated", "SELECT 1 -- bypass", "SELECT 1 INTO OUTFILE '/tmp/x'", "CALL danger()", "DROP TABLE risk_records", "DELETE FROM (SELECT 1) FOR UPDATE"} {
		if _, err := validateSQL(sql, true); err == nil {
			t.Errorf("unsafe SQL accepted: %s", sql)
		}
	}
	if _, err := validateSQL("DELETE FROM risk_records", false); err == nil {
		t.Fatal("read-only policy allowed a mutation")
	}
}
