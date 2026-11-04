package gormutil

import (
	"fmt"
	"strings"
	"time"

	"github.com/dujiao-next/internal/constants"

	"gorm.io/gorm"
)

var localizedJSONSearchKeys = append([]string(nil), constants.SupportedLocales...)

func DialectName(db *gorm.DB) string {
	if db == nil || db.Dialector == nil {
		return "sqlite"
	}
	name := strings.ToLower(strings.TrimSpace(db.Dialector.Name()))
	if name == "" {
		return "sqlite"
	}
	return name
}

func JSONTextExpr(db *gorm.DB, column, key string) string {
	return JSONTextExprByDialect(DialectName(db), column, key)
}

func JSONTextExprByDialect(dialect, column, key string) string {
	switch strings.ToLower(strings.TrimSpace(dialect)) {
	case "postgres", "postgresql":
		return fmt.Sprintf("(%s::jsonb ->> '%s')", column, key)
	default:
		return fmt.Sprintf("json_extract(%s, '$.\"%s\"')", column, key)
	}
}

func JSONArrayLengthExpr(db *gorm.DB, column string) string {
	return JSONArrayLengthExprByDialect(DialectName(db), column)
}

func JSONArrayLengthExprByDialect(dialect, column string) string {
	switch strings.ToLower(strings.TrimSpace(dialect)) {
	case "postgres", "postgresql":
		return fmt.Sprintf("jsonb_array_length(COALESCE(%s::jsonb, '[]'::jsonb))", column)
	default:
		return fmt.Sprintf("json_array_length(COALESCE(%s, '[]'))", column)
	}
}

// BuildLocalizedLikeCondition 构建普通列和多语言 JSON 列的模糊查询条件。
func BuildLocalizedLikeCondition(db *gorm.DB, plainColumns, jsonColumns []string) (string, int) {
	return BuildLocalizedLikeConditionByDialect(DialectName(db), plainColumns, jsonColumns)
}

func BuildLocalizedLikeConditionByDialect(dialect string, plainColumns, jsonColumns []string) (string, int) {
	parts := make([]string, 0, len(plainColumns)+len(jsonColumns)*len(localizedJSONSearchKeys))
	argCount := 0
	operator := "LIKE"
	if normalized := strings.ToLower(strings.TrimSpace(dialect)); normalized == "postgres" || normalized == "postgresql" {
		operator = "ILIKE"
	}

	for _, column := range plainColumns {
		trimmed := strings.TrimSpace(column)
		if trimmed == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s %s ?", trimmed, operator))
		argCount++
	}

	for _, column := range jsonColumns {
		trimmed := strings.TrimSpace(column)
		if trimmed == "" {
			continue
		}
		for _, key := range localizedJSONSearchKeys {
			parts = append(parts, fmt.Sprintf("%s %s ?", JSONTextExprByDialect(dialect, trimmed, key), operator))
			argCount++
		}
	}

	return strings.Join(parts, " OR "), argCount
}

func RepeatLikeArgs(like string, count int) []interface{} {
	args := make([]interface{}, 0, count)
	for i := 0; i < count; i++ {
		args = append(args, like)
	}
	return args
}

func ApplyPagination(query *gorm.DB, page, pageSize int) *gorm.DB {
	if query == nil || pageSize <= 0 {
		return query
	}
	if page < 1 {
		page = 1
	}
	offset := (page - 1) * pageSize
	if offset < 0 {
		offset = 0
	}
	return query.Limit(pageSize).Offset(offset)
}

// DefaultRetentionBatchSize 是分批清理的默认单批行数。
const DefaultRetentionBatchSize = 1000

// DeleteOlderThanBatched 分批删除 created_at 早于 before 的记录，返回删除总行数。
//
// 为什么分批：清理对象通常是历史日志表，积压可能几十万行。SQLite 运行在
// MaxOpenConns=1，一次性大 DELETE 会长时间占住唯一连接，期间所有业务请求
// 都被阻塞。按批删除并在批与批之间让出连接，可以避免这种停顿。
//
// SQL 采用 `id IN (SELECT ...)` 形式，SQLite 与 PostgreSQL 都支持；
// 不要用 DELETE ... LIMIT，SQLite 默认编译选项下不支持。
func DeleteOlderThanBatched(db *gorm.DB, model interface{}, before time.Time, batchSize int) (int64, error) {
	if db == nil {
		return 0, fmt.Errorf("db is nil")
	}
	if batchSize <= 0 {
		batchSize = DefaultRetentionBatchSize
	}

	var total int64
	for {
		sub := db.Model(model).
			Select("id").
			Where("created_at < ?", before).
			Limit(batchSize)

		result := db.Where("id IN (?)", sub).Delete(model)
		if result.Error != nil {
			return total, result.Error
		}
		total += result.RowsAffected
		if result.RowsAffected == 0 {
			return total, nil
		}
	}
}
