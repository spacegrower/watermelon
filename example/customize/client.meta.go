package firemelon

import (
	"net/url"
	"path/filepath"

	"google.golang.org/grpc/metadata"
)

// resolver service meta
type ResolveMeta struct {
	OrgID  string
	Region string
	System string
}

func (r ResolveMeta) FullServiceName(srvName string) string {
	target := filepath.ToSlash(filepath.Join(r.OrgID, r.System, srvName))
	path, _ := url.Parse(target)
	if r.Region != "" {
		query := path.Query()
		query.Add("region", r.Region)
		path.RawQuery = query.Encode()
	}
	return path.String()
}

func (r ResolveMeta) ProxyMetadata() metadata.MD {
	md := metadata.New(map[string]string{})
	if r.OrgID != "" {
		md.Set("fm-org-id", r.OrgID)
	}
	if r.Region != "" {
		md.Set("fm-region", r.Region)
	}
	if r.System != "" {
		md.Set("fm-system", r.System)
	}
	return md
}
