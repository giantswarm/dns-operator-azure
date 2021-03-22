package azure

type DNSSpec struct {
	ZoneName     string
	ARecords     []ARecord
	CNameRecords []CNameRecord
}

type ARecord struct {
	Hostname     string
	PublicIPName string
	TTL          int64
}

// CNameRecord specifies a DNS record mapping an alias to a canonical domain name.
type CNameRecord struct {
	Alias string
	CName string
	TTL   int64
}
