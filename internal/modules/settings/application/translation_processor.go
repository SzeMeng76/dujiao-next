package settingsapp

import (
	"context"
	"fmt"
	"strings"
	"time"

	settingsstore "github.com/dujiao-next/internal/modules/settings/infrastructure/gormstore"
	openaitranslate "github.com/dujiao-next/internal/modules/settings/infrastructure/openai"
	settingsintegration "github.com/dujiao-next/internal/modules/settings/schema/integration"
	"github.com/dujiao-next/internal/shared/jsonmap"
	"github.com/google/uuid"
)

// TranslationJobProcessor 翻译任务处理器
type TranslationJobProcessor struct {
	store    *settingsstore.Store
	client   openaitranslate.Client
	settings translationSettingGetter
}

type translationSettingGetter interface {
	GetTranslationSetting() (settingsintegration.TranslationSetting, error)
}

func NewTranslationJobProcessor(
	store *settingsstore.Store,
	client openaitranslate.Client,
	settings translationSettingGetter,
) *TranslationJobProcessor {
	return &TranslationJobProcessor{
		store:    store,
		client:   client,
		settings: settings,
	}
}

// SubmitJob 提交翻译任务
func (p *TranslationJobProcessor) SubmitJob(ctx context.Context, fields map[string]string) (string, error) {
	fieldsMap := make(jsonmap.JSON, len(fields))
	for key, text := range fields {
		fieldsMap[key] = text
	}

	job := &settingsstore.TranslationJobRecord{
		ID:        uuid.New().String(),
		Status:    settingsstore.TranslationJobStatusPending,
		Fields:    fieldsMap,
		Progress:  0,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := p.store.CreateTranslationJob(job); err != nil {
		return "", fmt.Errorf("create job: %w", err)
	}

	// 启动后台处理
	go p.processJobAsync(job.ID)

	return job.ID, nil
}

// GetJobStatus 查询任务状态
func (p *TranslationJobProcessor) GetJobStatus(ctx context.Context, jobID string) (interface{}, error) {
	job, err := p.store.GetTranslationJob(jobID)
	if err != nil {
		return nil, fmt.Errorf("get job: %w", err)
	}

	// 将存储层的 TranslationJobRecord 转换为 API 响应格式
	response := map[string]interface{}{
		"id":         job.ID,
		"status":     job.Status,
		"progress":   job.Progress,
		"created_at": job.CreatedAt,
		"updated_at": job.UpdatedAt,
	}

	if job.Status == settingsstore.TranslationJobStatusCompleted && job.Result != nil {
		response["result"] = job.Result
	}

	if job.Status == settingsstore.TranslationJobStatusFailed && job.Error != "" {
		response["error"] = job.Error
	}

	return response, nil
}

// processJobAsync 异步处理翻译任务（串行处理所有分组）
func (p *TranslationJobProcessor) processJobAsync(jobID string) {
	ctx := context.Background()

	job, err := p.store.GetTranslationJob(jobID)
	if err != nil {
		return
	}

	// 更新为处理中
	job.Status = settingsstore.TranslationJobStatusProcessing
	job.UpdatedAt = time.Now()
	_ = p.store.UpdateTranslationJob(job)

	// 解析待翻译字段
	fields := make(map[string]string, len(job.Fields))
	for key, value := range job.Fields {
		text, ok := value.(string)
		if !ok {
			continue
		}
		fields[key] = text
	}

	// 获取翻译配置
	setting, err := p.settings.GetTranslationSetting()
	if err != nil {
		p.failJob(job, fmt.Errorf("get setting: %w", err))
		return
	}

	if !setting.Enabled || setting.APIKey == "" {
		p.failJob(job, fmt.Errorf("translation not configured"))
		return
	}

	// 按长度分组：>1500 字切片；300-1500 字单独请求；≤300 字批量请求
	const longFieldThreshold = 300
	const chunkThreshold = 1500 // 超过此长度需要切片
	const chunkSize = 800       // 每个切片的字符数
	longFields := make(map[string]string)
	shortFields := make(map[string]string)
	chunkedFields := make(map[string][]string) // 需要切片的超长字段

	for key, text := range fields {
		if text == "" {
			continue
		}
		runes := []rune(text)
		runeCount := len(runes)

		if runeCount > chunkThreshold {
			// 超长字段切片
			var chunks []string
			for i := 0; i < runeCount; i += chunkSize {
				end := i + chunkSize
				if end > runeCount {
					end = runeCount
				}
				chunks = append(chunks, string(runes[i:end]))
			}
			chunkedFields[key] = chunks
		} else if runeCount > longFieldThreshold {
			longFields[key] = text
		} else {
			shortFields[key] = text
		}
	}

	// 计算总组数
	totalGroups := len(longFields) + len(chunkedFields)
	for _, chunks := range chunkedFields {
		totalGroups += len(chunks) - 1 // 每个切片字段的多个chunk
	}
	if len(shortFields) > 0 {
		totalGroups++
	}
	completedGroups := 0
	result := make(map[string]map[string]string)

	// 串行处理短字段批量请求（如果有）
	if len(shortFields) > 0 {
		items := make([]openaitranslate.Item, 0, len(shortFields))
		for key, text := range shortFields {
			items = append(items, openaitranslate.Item{Key: key, Text: text})
		}

		groupResult, err := p.client.Translate(ctx, setting, items)
		if err != nil {
			p.failJob(job, fmt.Errorf("translate short fields: %w", err))
			return
		}

		for key, translations := range groupResult {
			result[key] = translations
		}

		completedGroups++
		job.Progress = (completedGroups * 100) / totalGroups
		job.UpdatedAt = time.Now()
		_ = p.store.UpdateTranslationJob(job)
	}

	// 串行处理长字段（每个单独请求）
	for key, text := range longFields {
		items := []openaitranslate.Item{{Key: key, Text: text}}

		groupResult, err := p.client.Translate(ctx, setting, items)
		if err != nil {
			p.failJob(job, fmt.Errorf("translate field %s: %w", key, err))
			return
		}

		for k, translations := range groupResult {
			result[k] = translations
		}

		completedGroups++
		job.Progress = (completedGroups * 100) / totalGroups
		job.UpdatedAt = time.Now()
		_ = p.store.UpdateTranslationJob(job)
	}

	// 串行处理超长切片字段（每个切片单独请求）
	for key, chunks := range chunkedFields {
		// 每个语言单独拼接
		zwTWParts := make([]string, 0, len(chunks))
		enUSParts := make([]string, 0, len(chunks))

		for i, chunk := range chunks {
			chunkKey := fmt.Sprintf("%s_chunk_%d", key, i)
			items := []openaitranslate.Item{{Key: chunkKey, Text: chunk}}

			groupResult, err := p.client.Translate(ctx, setting, items)
			if err != nil {
				p.failJob(job, fmt.Errorf("translate field %s chunk %d: %w", key, i, err))
				return
			}

			if translations, ok := groupResult[chunkKey]; ok {
				if zwTW, ok := translations["zh-TW"]; ok {
					zwTWParts = append(zwTWParts, zwTW)
				}
				if enUS, ok := translations["en-US"]; ok {
					enUSParts = append(enUSParts, enUS)
				}
			}

			completedGroups++
			job.Progress = (completedGroups * 100) / totalGroups
			job.UpdatedAt = time.Now()
			_ = p.store.UpdateTranslationJob(job)
		}

		// 拼接所有切片的翻译结果
		result[key] = map[string]string{
			"zh-TW": strings.Join(zwTWParts, ""),
			"en-US": strings.Join(enUSParts, ""),
		}
	}

	// 完成任务
	resultMap := make(jsonmap.JSON, len(result))
	for key, translations := range result {
		resultMap[key] = translations
	}

	job.Status = settingsstore.TranslationJobStatusCompleted
	job.Result = resultMap
	job.Progress = 100
	job.UpdatedAt = time.Now()
	_ = p.store.UpdateTranslationJob(job)
}

func (p *TranslationJobProcessor) failJob(job *settingsstore.TranslationJobRecord, err error) {
	job.Status = settingsstore.TranslationJobStatusFailed
	job.Error = err.Error()
	job.UpdatedAt = time.Now()
	_ = p.store.UpdateTranslationJob(job)
}
