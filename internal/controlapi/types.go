package controlapi

import "time"

type ResourceKind string

const (
	KindNode           ResourceKind = "node"
	KindInbound        ResourceKind = "inbound"
	KindOutbound       ResourceKind = "outbound"
	KindRoutingPolicy  ResourceKind = "routing_policy"
	KindClient         ResourceKind = "client"
	KindCertificate    ResourceKind = "certificate"
	KindConfigRevision ResourceKind = "config_revision"
)

var managedKinds = []ResourceKind{
	KindNode,
	KindInbound,
	KindOutbound,
	KindRoutingPolicy,
	KindClient,
	KindCertificate,
}

type User struct {
	ID                 string    `json:"id"`
	Username           string    `json:"username"`
	Role               string    `json:"role"`
	MustChangePassword bool      `json:"mustChangePassword"`
	CreatedAt          time.Time `json:"createdAt"`
	UpdatedAt          time.Time `json:"updatedAt"`
}

type AuthenticatedUser struct {
	ID                 string `json:"id"`
	Username           string `json:"username"`
	Role               string `json:"role"`
	MustChangePassword bool   `json:"mustChangePassword"`
}

type ServiceToken struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Role       string     `json:"role"`
	Prefix     string     `json:"prefix"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
}

type ServiceTokenRecord struct {
	ServiceToken
	TokenHash string
}

type CreatedServiceToken struct {
	ServiceToken
	RawToken string `json:"rawToken"`
}

type ManagedResource struct {
	ID        string         `json:"id"`
	Kind      ResourceKind   `json:"kind"`
	NodeID    *string        `json:"nodeId,omitempty"`
	Name      string         `json:"name"`
	Slug      string         `json:"slug"`
	Protocol  *string        `json:"protocol,omitempty"`
	Port      *int           `json:"port,omitempty"`
	IsEnabled bool           `json:"isEnabled"`
	Status    string         `json:"status"`
	Spec      map[string]any `json:"spec"`
	Metadata  map[string]any `json:"metadata"`
	Revision  string         `json:"revision"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
}

type ResourceInput struct {
	NodeID    *string        `json:"nodeId,omitempty"`
	Name      string         `json:"name"`
	Slug      string         `json:"slug,omitempty"`
	Protocol  *string        `json:"protocol,omitempty"`
	Port      *int           `json:"port,omitempty"`
	IsEnabled bool           `json:"isEnabled"`
	Status    string         `json:"status,omitempty"`
	Spec      map[string]any `json:"spec,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

type ConfigRevision struct {
	ID         string         `json:"id"`
	ResourceID string         `json:"resourceId"`
	Kind       ResourceKind   `json:"kind"`
	Revision   string         `json:"revision"`
	Snapshot   map[string]any `json:"snapshot"`
	CreatedAt  time.Time      `json:"createdAt"`
}

type BackupJob struct {
	ID         string         `json:"id"`
	FilePath   string         `json:"filePath"`
	Status     string         `json:"status"`
	Metadata   map[string]any `json:"metadata"`
	CreatedAt  time.Time      `json:"createdAt"`
	FinishedAt *time.Time     `json:"finishedAt,omitempty"`
}

type BackupUserRecord struct {
	User
	PasswordHash string `json:"passwordHash"`
}

type BackupServiceTokenRecord struct {
	ServiceToken
	TokenHash string `json:"tokenHash"`
}

type BackupSnapshot struct {
	ExportedAt time.Time                    `json:"exportedAt"`
	Users      []BackupUserRecord           `json:"users"`
	Resources  map[string][]ManagedResource `json:"resources"`
	Audit      []AuditEntry                 `json:"audit"`
	Revisions  []ConfigRevision             `json:"revisions"`
	Tokens     []BackupServiceTokenRecord   `json:"tokens"`
}

type AuditEntry struct {
	ID           string         `json:"id"`
	ActorType    string         `json:"actorType"`
	ActorID      string         `json:"actorId"`
	Action       string         `json:"action"`
	ResourceType string         `json:"resourceType"`
	ResourceID   string         `json:"resourceId"`
	Payload      map[string]any `json:"payload"`
	RemoteAddr   string         `json:"remoteAddr"`
	CreatedAt    time.Time      `json:"createdAt"`
}

type DashboardStats struct {
	ResourceCounts map[string]int `json:"resourceCounts"`
	TotalNodes     int            `json:"totalNodes"`
	OnlineNodes    int            `json:"onlineNodes"`
}

type MetricPoint struct {
	Label string `json:"label"`
	Value int64  `json:"value"`
}

type SessionEvent struct {
	EventID     string         `json:"eventId"`
	NodeID      string         `json:"nodeId"`
	EventType   string         `json:"eventType"`
	Protocol    string         `json:"protocol"`
	InboundID   *string        `json:"inboundId,omitempty"`
	OutboundID  *string        `json:"outboundId,omitempty"`
	ClientID    *string        `json:"clientId,omitempty"`
	BytesUp     int64          `json:"bytesUp"`
	BytesDown   int64          `json:"bytesDown"`
	Destination string         `json:"destination"`
	RuleTag     string         `json:"ruleTag"`
	Status      string         `json:"status"`
	Payload     map[string]any `json:"payload,omitempty"`
	CreatedAt   time.Time      `json:"createdAt"`
}

type AnalyticsSummary struct {
	Window          string         `json:"window"`
	TotalEvents     int64          `json:"totalEvents"`
	TotalBytes      int64          `json:"totalBytes"`
	TopProtocols    []MetricPoint  `json:"topProtocols"`
	TopDestinations []MetricPoint  `json:"topDestinations"`
	RecentEvents    []SessionEvent `json:"recentEvents"`
}

type ConfigBundle struct {
	NodeID      string                       `json:"nodeId"`
	GeneratedAt time.Time                    `json:"generatedAt"`
	Resources   map[string][]ManagedResource `json:"resources"`
}

type ClientArtifact struct {
	ClientID      string  `json:"clientId"`
	DisplayName   string  `json:"displayName"`
	DownloadName  string  `json:"downloadName"`
	ArtifactText  string  `json:"artifactText"`
	QRText        string  `json:"qrText"`
	Protocol      *string `json:"protocol,omitempty"`
	CurrentStatus string  `json:"currentStatus"`
	StatusReason  string  `json:"statusReason,omitempty"`
	ResolvedHost  string  `json:"resolvedHost,omitempty"`
	ResolvedPort  int     `json:"resolvedPort,omitempty"`
}

type NodeSummary struct {
	ID              string         `json:"id"`
	Name            string         `json:"name"`
	PublicAddress   string         `json:"publicAddress"`
	Status          string         `json:"status"`
	IsEnabled       bool           `json:"isEnabled"`
	IsBuiltin       bool           `json:"isBuiltin"`
	IsLocal         bool           `json:"isLocal"`
	CurrentRevision string         `json:"currentRevision"`
	AssignedPorts   map[string]int `json:"assignedPorts"`
	LastSeenAt      string         `json:"lastSeenAt"`
	Health          map[string]any `json:"health"`
	UpdatedAt       time.Time      `json:"updatedAt"`
}

type InboundTLSSettings struct {
	Enabled    bool     `json:"enabled"`
	ServerName string   `json:"serverName"`
	ALPN       []string `json:"alpn"`
}

type InboundRealitySettings struct {
	Enabled          bool     `json:"enabled"`
	Show             bool     `json:"show"`
	Xver             int      `json:"xver"`
	UTLSFingerprint  string   `json:"utlsFingerprint"`
	Target           string   `json:"target"`
	ServerNames      []string `json:"serverNames"`
	MaxTimeDiffMs    int      `json:"maxTimeDiffMs"`
	MinClientVersion string   `json:"minClientVersion"`
	MaxClientVersion string   `json:"maxClientVersion"`
	ShortIDs         []string `json:"shortIds"`
	SpiderX          string   `json:"spiderX"`
	PublicKey        string   `json:"publicKey"`
	PrivateKey       string   `json:"privateKey"`
	MLDSA65Seed      string   `json:"mldsa65Seed"`
	MLDSA65Verify    string   `json:"mldsa65Verify"`
	PQV              string   `json:"pqv"`
}

type InboundEgressChain struct {
	Enabled             bool     `json:"enabled"`
	HopInboundIDs       []string `json:"hopInboundIds"`
	TerminalOutboundTag string   `json:"terminalOutboundTag"`
}

type InboundRecord struct {
	ID                 string                 `json:"id"`
	Name               string                 `json:"name"`
	NodeID             string                 `json:"nodeId"`
	NodeName           string                 `json:"nodeName"`
	Protocol           string                 `json:"protocol"`
	Port               *int                   `json:"port,omitempty"`
	IsEnabled          bool                   `json:"isEnabled"`
	Status             string                 `json:"status"`
	Listen             string                 `json:"listen"`
	Transport          string                 `json:"transport"`
	Security           string                 `json:"security"`
	ServerName         string                 `json:"serverName"`
	SNI                string                 `json:"sni"`
	PublicAddress      string                 `json:"publicAddress"`
	Obfs               string                 `json:"obfs"`
	UpMbps             int                    `json:"upMbps"`
	DownMbps           int                    `json:"downMbps"`
	LocalAddress       []string               `json:"localAddress"`
	PrivateKey         string                 `json:"privateKey"`
	TLSSettings        InboundTLSSettings     `json:"tlsSettings"`
	RealitySettings    InboundRealitySettings `json:"realitySettings"`
	EgressChain        InboundEgressChain     `json:"egressChain"`
	ResolvedPublicHost string                 `json:"resolvedPublicHost"`
	CertificateID      string                 `json:"certificateId"`
	CertificateStatus  string                 `json:"certificateStatus"`
	ClientCount        int                    `json:"clientCount"`
	Tag                string                 `json:"tag"`
	UpdatedAt          time.Time              `json:"updatedAt"`
}

type InboundUpsertRequest struct {
	Name            string                  `json:"name"`
	NodeID          string                  `json:"nodeId,omitempty"`
	Protocol        string                  `json:"protocol"`
	Port            *int                    `json:"port,omitempty"`
	IsEnabled       bool                    `json:"isEnabled"`
	Listen          string                  `json:"listen,omitempty"`
	Transport       string                  `json:"transport,omitempty"`
	Security        string                  `json:"security,omitempty"`
	ServerName      string                  `json:"serverName,omitempty"`
	SNI             string                  `json:"sni,omitempty"`
	PublicAddress   string                  `json:"publicAddress,omitempty"`
	Obfs            string                  `json:"obfs,omitempty"`
	UpMbps          int                     `json:"upMbps,omitempty"`
	DownMbps        int                     `json:"downMbps,omitempty"`
	LocalAddress    []string                `json:"localAddress,omitempty"`
	PrivateKey      string                  `json:"privateKey,omitempty"`
	TLSSettings     *InboundTLSSettings     `json:"tlsSettings,omitempty"`
	RealitySettings *InboundRealitySettings `json:"realitySettings,omitempty"`
	EgressChain     *InboundEgressChain     `json:"egressChain,omitempty"`
}

type InboundPresetRequest struct {
	NodeID       string `json:"nodeId"`
	Protocol     string `json:"protocol"`
	SecurityMode string `json:"securityMode"`
}

type InboundPresetResponse struct {
	Payload InboundUpsertRequest `json:"payload"`
}

type OutboundRecord struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Tag           string    `json:"tag"`
	Protocol      string    `json:"protocol"`
	IsEnabled     bool      `json:"isEnabled"`
	Status        string    `json:"status"`
	Host          string    `json:"host"`
	Port          int       `json:"port"`
	Username      string    `json:"username"`
	Password      string    `json:"password"`
	ServerName    string    `json:"serverName"`
	Identity      string    `json:"identity"`
	Outbounds     []string  `json:"outbounds"`
	Default       string    `json:"default"`
	URL           string    `json:"url"`
	Interval      string    `json:"interval"`
	LocalAddress  []string  `json:"localAddress"`
	PrivateKey    string    `json:"privateKey"`
	PeerPublicKey string    `json:"peerPublicKey"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

type OutboundUpsertRequest struct {
	Name          string   `json:"name"`
	Protocol      string   `json:"protocol"`
	IsEnabled     bool     `json:"isEnabled"`
	Host          string   `json:"host,omitempty"`
	Port          int      `json:"port,omitempty"`
	Username      string   `json:"username,omitempty"`
	Password      string   `json:"password,omitempty"`
	ServerName    string   `json:"serverName,omitempty"`
	Identity      string   `json:"identity,omitempty"`
	Outbounds     []string `json:"outbounds,omitempty"`
	Default       string   `json:"default,omitempty"`
	URL           string   `json:"url,omitempty"`
	Interval      string   `json:"interval,omitempty"`
	LocalAddress  []string `json:"localAddress,omitempty"`
	PrivateKey    string   `json:"privateKey,omitempty"`
	PeerPublicKey string   `json:"peerPublicKey,omitempty"`
}

type RoutingPolicyRecord struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	IsEnabled      bool      `json:"isEnabled"`
	Status         string    `json:"status"`
	InboundTags    []string  `json:"inboundTags"`
	Protocols      []string  `json:"protocols"`
	Domains        []string  `json:"domains"`
	DomainSuffixes []string  `json:"domainSuffixes"`
	IPCidrs        []string  `json:"ipCidrs"`
	Ports          string    `json:"ports"`
	Network        string    `json:"network"`
	OutboundTag    string    `json:"outboundTag"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type RoutingPolicyUpsertRequest struct {
	Name           string   `json:"name"`
	IsEnabled      bool     `json:"isEnabled"`
	InboundTags    []string `json:"inboundTags,omitempty"`
	Protocols      []string `json:"protocols,omitempty"`
	Domains        []string `json:"domains,omitempty"`
	DomainSuffixes []string `json:"domainSuffixes,omitempty"`
	IPCidrs        []string `json:"ipCidrs,omitempty"`
	Ports          string   `json:"ports,omitempty"`
	Network        string   `json:"network,omitempty"`
	OutboundTag    string   `json:"outboundTag"`
}

type CertificateRecord struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	NodeID      string    `json:"nodeId"`
	NodeName    string    `json:"nodeName"`
	IsEnabled   bool      `json:"isEnabled"`
	Status      string    `json:"status"`
	Type        string    `json:"type"`
	Subject     string    `json:"subject"`
	Provider    string    `json:"provider"`
	Domains     []string  `json:"domains"`
	IPAddresses []string  `json:"ipAddresses"`
	Email       string    `json:"email"`
	CertFile    string    `json:"certFile"`
	KeyFile     string    `json:"keyFile"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type CertificateUpsertRequest struct {
	Name        string   `json:"name"`
	NodeID      string   `json:"nodeId,omitempty"`
	IsEnabled   bool     `json:"isEnabled"`
	Type        string   `json:"type"`
	Subject     string   `json:"subject"`
	Provider    string   `json:"provider,omitempty"`
	Domains     []string `json:"domains,omitempty"`
	IPAddresses []string `json:"ipAddresses,omitempty"`
	Email       string   `json:"email,omitempty"`
}

type InboundClientRecord struct {
	ID                  string    `json:"id"`
	InboundID           string    `json:"inboundId"`
	Name                string    `json:"name"`
	Protocol            string    `json:"protocol"`
	IsEnabled           bool      `json:"isEnabled"`
	Status              string    `json:"status"`
	Username            string    `json:"username"`
	Password            string    `json:"password"`
	UUID                string    `json:"uuid"`
	Flow                string    `json:"flow"`
	PrivateKey          string    `json:"privateKey"`
	PresharedKey        string    `json:"presharedKey"`
	Addresses           []string  `json:"addresses"`
	AllowedIPs          []string  `json:"allowedIps"`
	PersistentKeepalive string    `json:"persistentKeepalive"`
	UpdatedAt           time.Time `json:"updatedAt"`
}

type InboundClientUpsertRequest struct {
	Name                string   `json:"name"`
	IsEnabled           bool     `json:"isEnabled"`
	Username            string   `json:"username,omitempty"`
	Password            string   `json:"password,omitempty"`
	UUID                string   `json:"uuid,omitempty"`
	Flow                string   `json:"flow,omitempty"`
	PrivateKey          string   `json:"privateKey,omitempty"`
	PresharedKey        string   `json:"presharedKey,omitempty"`
	Addresses           []string `json:"addresses,omitempty"`
	AllowedIPs          []string `json:"allowedIps,omitempty"`
	PersistentKeepalive string   `json:"persistentKeepalive,omitempty"`
}

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type LoginResponse struct {
	User  AuthenticatedUser `json:"user"`
	Token string            `json:"token"`
}

type ChangePasswordRequest struct {
	OldPassword string `json:"oldPassword"`
	NewPassword string `json:"newPassword"`
}

type CreateServiceTokenRequest struct {
	Name string `json:"name"`
	Role string `json:"role"`
}

type AgentRegisterRequest struct {
	NodeID        string            `json:"nodeId,omitempty"`
	Name          string            `json:"name"`
	PublicAddress string            `json:"publicAddress"`
	Labels        map[string]string `json:"labels,omitempty"`
	Version       string            `json:"version,omitempty"`
}

type AgentRegisterResponse struct {
	NodeID string `json:"nodeId"`
}

type AgentHeartbeatRequest struct {
	NodeID          string         `json:"nodeId"`
	NodeName        string         `json:"nodeName"`
	PublicAddress   string         `json:"publicAddress"`
	Version         string         `json:"version"`
	Status          string         `json:"status,omitempty"`
	CurrentRevision string         `json:"currentRevision"`
	AssignedPorts   map[string]int `json:"assignedPorts"`
	Health          map[string]any `json:"health,omitempty"`
}

type AgentTelemetryRequest struct {
	NodeID string         `json:"nodeId"`
	Events []SessionEvent `json:"events"`
}
