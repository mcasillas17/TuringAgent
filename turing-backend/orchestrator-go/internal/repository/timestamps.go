package repository

import (
	"strings"
	"time"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/persisttime"
)

func FormatTimestamp(value time.Time) string {
	return persisttime.Format(value)
}

func sqliteTimestampNanos(column string) string {
	return strings.ReplaceAll(`(
		CAST(strftime('%s', substr(__timestamp__, 1, 19) || 'Z') AS INTEGER) * 1000000000 +
		CASE
			WHEN instr(__timestamp__, '.') = 0 THEN 0
			ELSE CAST(substr(
				substr(__timestamp__, instr(__timestamp__, '.') + 1, length(__timestamp__) - instr(__timestamp__, '.') - 1) || '000000000',
				1,
				9
			) AS INTEGER)
		END
	)`, "__timestamp__", column)
}
