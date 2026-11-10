package blob

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/xingshanghe/x-phone/internal/config"
)

type Store interface {
	Put(ctx context.Context, key string, r io.Reader, size int64) error
	Open(ctx context.Context, key string) (io.ReadCloser, error)
}

func New(cfg config.Config) (Store, error) {
	if cfg.MinioEndpoint == "" {
		return NewFile(filepath.Join(cfg.DataDir, "blobs")), nil
	}
	client, err := minio.New(cfg.MinioEndpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.MinioAccessKey, cfg.MinioSecretKey, ""),
		Secure: cfg.MinioSSL,
	})
	if err != nil {
		return nil, err
	}
	store := &Minio{client: client, bucket: cfg.MinioBucket}
	ctx := context.Background()
	exists, err := client.BucketExists(ctx, cfg.MinioBucket)
	if err != nil {
		return nil, err
	}
	if !exists {
		if err := client.MakeBucket(ctx, cfg.MinioBucket, minio.MakeBucketOptions{}); err != nil {
			return nil, err
		}
	}
	return store, nil
}

type File struct {
	root string
}

func NewFile(root string) *File {
	return &File{root: root}
}

func (f *File) Put(_ context.Context, key string, r io.Reader, _ int64) error {
	path, err := f.path(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.Copy(file, r)
	return err
}

func (f *File) Open(_ context.Context, key string) (io.ReadCloser, error) {
	path, err := f.path(key)
	if err != nil {
		return nil, err
	}
	return os.Open(path)
}

func (f *File) path(key string) (string, error) {
	clean, err := cleanKey(key)
	if err != nil {
		return "", err
	}
	return filepath.Join(f.root, filepath.FromSlash(clean)), nil
}

type Minio struct {
	client *minio.Client
	bucket string
}

func (m *Minio) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	clean, err := cleanKey(key)
	if err != nil {
		return err
	}
	_, err = m.client.PutObject(ctx, m.bucket, clean, r, size, minio.PutObjectOptions{})
	return err
}

func (m *Minio) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	clean, err := cleanKey(key)
	if err != nil {
		return nil, err
	}
	return m.client.GetObject(ctx, m.bucket, clean, minio.GetObjectOptions{})
}

func cleanKey(key string) (string, error) {
	if strings.Contains(key, "..") || strings.HasPrefix(key, "/") {
		return "", os.ErrInvalid
	}
	clean := pathClean(key)
	if clean == "." || strings.Contains(clean, "..") {
		return "", os.ErrInvalid
	}
	return clean, nil
}

func pathClean(key string) string {
	return strings.Trim(filepath.ToSlash(filepath.Clean(key)), "/")
}
