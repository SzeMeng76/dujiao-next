package settingsstore

import (
	"time"

	"github.com/dujiao-next/internal/shared/jsonmap"
)

// TranslationJobStatus 翻译任务状态
type TranslationJobStatus string

const (
	TranslationJobStatusPending    TranslationJobStatus = "pending"
	TranslationJobStatusProcessing TranslationJobStatus = "processing"
	TranslationJobStatusCompleted  TranslationJobStatus = "completed"
	TranslationJobStatusFailed     TranslationJobStatus = "failed"
)

// TranslationJobRecord 翻译任务持久化记录
type TranslationJobRecord struct {
	ID        string               `gorm:"primarykey;size:64" json:"id"`
	Status    TranslationJobStatus `gorm:"size:20;not null;index" json:"status"`
	Fields    jsonmap.JSON         `gorm:"type:json;not null" json:"fields"`      // 待翻译字段 map[string]string
	Result    jsonmap.JSON         `gorm:"type:json" json:"result"`               // 翻译结果 map[string]map[string]string
	Error     string               `gorm:"type:text" json:"error"`                // 错误信息
	Progress  int                  `gorm:"not null;default:0" json:"progress"`    // 进度百分比 0-100
	CreatedAt time.Time            `gorm:"not null" json:"created_at"`
	UpdatedAt time.Time            `gorm:"not null" json:"updated_at"`
}

func (TranslationJobRecord) TableName() string {
	return "translation_jobs"
}

// CreateTranslationJob 创建翻译任务
func (store *Store) CreateTranslationJob(job *TranslationJobRecord) error {
	return store.db.Create(job).Error
}

// GetTranslationJob 根据 ID 查询翻译任务
func (store *Store) GetTranslationJob(id string) (*TranslationJobRecord, error) {
	var job TranslationJobRecord
	if err := store.db.Where("id = ?", id).First(&job).Error; err != nil {
		return nil, err
	}
	return &job, nil
}

// UpdateTranslationJob 更新翻译任务
func (store *Store) UpdateTranslationJob(job *TranslationJobRecord) error {
	return store.db.Save(job).Error
}

// DeleteOldTranslationJobs 删除超过指定天数的已完成/失败任务
func (store *Store) DeleteOldTranslationJobs(days int) error {
	cutoff := time.Now().AddDate(0, 0, -days)
	return store.db.Where("status IN ? AND updated_at < ?",
		[]TranslationJobStatus{TranslationJobStatusCompleted, TranslationJobStatusFailed},
		cutoff).Delete(&TranslationJobRecord{}).Error
}
