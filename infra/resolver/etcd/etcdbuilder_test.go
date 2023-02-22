package etcd

import (
	"net/url"
	"path/filepath"
	"testing"
)

func TestGenTarget(t *testing.T) {
	region := "local"
	target := filepath.ToSlash(filepath.Join("space", "fw", "watermelon"))
	path, _ := url.Parse(target)
	if region != "" {
		query := path.Query()
		query.Add("region", region)
		path.RawQuery = query.Encode()
	}
	t.Log(path.Path, path.RawPath, path.String())
}
