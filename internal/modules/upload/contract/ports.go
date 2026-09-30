package contract

import "io"

// Result 是上传成功后的文件元数据。
type Result struct {
	URL      string
	Filename string
	MimeType string
	Size     int64
	Width    int
	Height   int
}

// StoreInput 描述本地存储文件所需的信息。
type StoreInput struct {
	Source   io.Reader
	Scene    string
	Year     string
	Month    string
	Filename string
}

// Store 是上传应用层写入文件所需的端口。
type Store interface {
	Save(input StoreInput) (publicURL string, err error)
	// Delete 删除一个先前由 Save 返回的公开 URL 对应的文件。文件不存在时静默忽略。
	Delete(publicURL string) error
}
