package seoguidepostreader

import (
	"context"

	contentapp "github.com/dujiao-next/internal/modules/content/application"
	seoguidecontract "github.com/dujiao-next/internal/modules/seoguide/contract"
)

// Reader 将 content.PostService 适配为 seoguide 的 PostReader 端口。
type Reader struct {
	svc *contentapp.PostService
}

var _ seoguidecontract.PostReader = (*Reader)(nil)

// New 创建文章读取适配器。
func New(svc *contentapp.PostService) *Reader {
	if svc == nil {
		panic("seoguide post reader: post service is nil")
	}
	return &Reader{svc: svc}
}

// ListPublishedPosts 返回最近已发布文章的 SEO 投影。
func (r *Reader) ListPublishedPosts(limit int) ([]seoguidecontract.Post, error) {
	posts, _, err := r.svc.ListPublic(context.Background(), contentapp.PublicPostQuery{
		Page:     1,
		PageSize: limit,
	})
	if err != nil {
		return nil, err
	}
	result := make([]seoguidecontract.Post, 0, len(posts))
	for _, p := range posts {
		result = append(result, seoguidecontract.Post{
			Slug:        p.Slug,
			Title:       map[string]interface{}(p.TitleJSON),
			Description: map[string]interface{}(p.SummaryJSON),
			PublishedAt: p.PublishedAt,
			CreatedAt:   p.CreatedAt,
		})
	}
	return result, nil
}
