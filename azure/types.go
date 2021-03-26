package azure

type DNSSpec struct {
	ZoneName        string
	ARecordSets     []ARecordSetSpec
	CNameRecordSets []CNameRecordSetSpec
	NSRecordSets    []NSRecordSetSpec
}

type ARecordSetSpec struct {
	Hostname     string
	PublicIPName string
	TTL          int64
}

// CNameRecordSetSpec specifies a DNS record mapping an alias to a canonical domain name.
type CNameRecordSetSpec struct {
	Alias string
	CName string
	TTL   int64
}

type NSDomainNameSpec struct {
	NSDomainName string
}

type NSRecordSetSpec struct {
	Name          string
	NSDomainNames []NSDomainNameSpec
	TTL           int64
}
