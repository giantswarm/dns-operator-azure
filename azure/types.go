package azure

type DNSSpec struct {
	ZoneName     string
	ARecordSets  []ARecordSetSpec
	NSRecordSets []NSRecordSetSpec
}

type ARecordSetSpec struct {
	Hostname     string
	PublicIPName string
	TTL          int64
}

type NSDomainNameSpec struct {
	NSDomainName string
}

type NSRecordSetSpec struct {
	Name          string
	NSDomainNames []NSDomainNameSpec
	TTL           int64
}
