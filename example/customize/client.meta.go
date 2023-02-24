package firemelon

import (
	"net/url"
	"path/filepath"
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
