package llm

import (
	"fmt"
	"strings"
)

// sqlTokens rejects comments, unterminated literals and stacked statements.
// Quoted payloads stay data; quoted identifiers remain visible to the policy.
func sqlTokens(sql string) ([]string, error) {
	var tokens []string
	for i := 0; i < len(sql); {
		c := sql[i]
		if c == ';' {
			if strings.TrimSpace(sql[i+1:]) != "" {
				return nil, fmt.Errorf("only one SQL statement is allowed")
			}
			break
		}
		if c == '#' || c == '/' && i+1 < len(sql) && sql[i+1] == '*' || c == '-' && i+1 < len(sql) && sql[i+1] == '-' {
			return nil, fmt.Errorf("SQL comments are not allowed")
		}
		if c == '\'' || c == '"' || c == '`' {
			quote := c
			start := i + 1
			i++
			closed := false
			for i < len(sql) {
				if sql[i] == '\\' && quote != '`' {
					i += 2
					continue
				}
				if sql[i] == quote {
					if i+1 < len(sql) && sql[i+1] == quote {
						i += 2
						continue
					}
					if quote == '`' {
						tokens = append(tokens, strings.ToLower(sql[start:i]))
					}
					i++
					closed = true
					break
				}
				i++
			}
			if !closed {
				return nil, fmt.Errorf("unterminated SQL literal")
			}
			continue
		}
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_' {
			start := i
			i++
			for i < len(sql) && (sql[i] >= 'a' && sql[i] <= 'z' || sql[i] >= 'A' && sql[i] <= 'Z' || sql[i] >= '0' && sql[i] <= '9' || sql[i] == '_') {
				i++
			}
			tokens = append(tokens, strings.ToLower(sql[start:i]))
			continue
		}
		i++
	}
	return tokens, nil
}

func validateSQL(sql string, allowWrites bool) (string, error) {
	tokens, err := sqlTokens(sql)
	if err != nil {
		return "", err
	}
	if len(tokens) == 0 {
		return "", fmt.Errorf("empty SQL")
	}
	first := tokens[0]
	switch first {
	case "select", "show", "describe", "desc", "explain", "with":
	case "insert", "update", "delete":
		if !allowWrites {
			return "", fmt.Errorf("SQL writes are disabled")
		}
	default:
		return "", fmt.Errorf("unsupported SQL statement %q", first)
	}
	write := first == "insert" || first == "update" || first == "delete"
	for _, token := range tokens {
		if _, forbidden := forbiddenKeywordSet[token]; forbidden && !(token == "set" && first == "update") {
			return "", fmt.Errorf("forbidden SQL token %q", token)
		}
		if token == "sleep" || token == "benchmark" || token == "get_lock" || token == "release_lock" || token == "mysql" || token == "performance_schema" || token == "sys" || token == "for" {
			return "", fmt.Errorf("forbidden SQL token %q", token)
		}
		if !write && (token == "insert" || token == "update" || token == "delete" || token == "replace") {
			return "", fmt.Errorf("embedded SQL mutation is not allowed")
		}
		if write && (strings.HasPrefix(token, "taskflow_") || token == "executions" || token == "scheduled_tasks" || token == "condition_tasks" || token == "tcc_tx_records" || token == "mq_dead_letters") {
			return "", fmt.Errorf("taskflow control tables cannot be mutated by tools")
		}
	}
	return first, nil
}
