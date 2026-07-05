package model

import (
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	MinSegmentDurationSeconds int64 = 60
	MaxSegmentDurationSeconds int64 = 24 * 60 * 60
)

type Camera struct {
	ID                     string    `json:"id"`
	Name                   string    `json:"name"`
	Host                   string    `json:"host"`
	Username               string    `json:"username,omitempty"`
	Password               string    `json:"password,omitempty"`
	RTSPURL                string    `json:"rtsp_url"`
	LiveRTSPURL            string    `json:"live_rtsp_url,omitempty"`
	ProfileToken           string    `json:"profile_token,omitempty"`
	LiveProfileToken       string    `json:"live_profile_token,omitempty"`
	Manufacturer           string    `json:"manufacturer,omitempty"`
	Model                  string    `json:"model,omitempty"`
	SerialNumber           string    `json:"serial_number,omitempty"`
	FirmwareVersion        string    `json:"firmware_version,omitempty"`
	Codec                  string    `json:"codec,omitempty"`
	Width                  int       `json:"width,omitempty"`
	Height                 int       `json:"height,omitempty"`
	LiveCodec              string    `json:"live_codec,omitempty"`
	LiveWidth              int       `json:"live_width,omitempty"`
	LiveHeight             int       `json:"live_height,omitempty"`
	FolderName             string    `json:"folder_name"`
	Enabled                bool      `json:"enabled"`
	Record                 bool      `json:"record"`
	SegmentDurationSeconds int64     `json:"segment_duration_seconds"`
	Upload                 bool      `json:"upload"`
	SFTPProfileID          string    `json:"sftp_profile_id,omitempty"`
	AudioCodec             string    `json:"audio_codec,omitempty"`
	AudioSampleRate        int       `json:"audio_sample_rate,omitempty"`
	AudioChannels          int       `json:"audio_channels,omitempty"`
	CreatedAt              time.Time `json:"created_at"`
	UpdatedAt              time.Time `json:"updated_at"`
}

type CameraPublic struct {
	ID                     string    `json:"id"`
	Name                   string    `json:"name"`
	Host                   string    `json:"host"`
	Username               string    `json:"username,omitempty"`
	RTSPURL                string    `json:"rtsp_url"`
	LiveRTSPURL            string    `json:"live_rtsp_url,omitempty"`
	ProfileToken           string    `json:"profile_token,omitempty"`
	LiveProfileToken       string    `json:"live_profile_token,omitempty"`
	Manufacturer           string    `json:"manufacturer,omitempty"`
	Model                  string    `json:"model,omitempty"`
	SerialNumber           string    `json:"serial_number,omitempty"`
	FirmwareVersion        string    `json:"firmware_version,omitempty"`
	Codec                  string    `json:"codec,omitempty"`
	Width                  int       `json:"width,omitempty"`
	Height                 int       `json:"height,omitempty"`
	LiveCodec              string    `json:"live_codec,omitempty"`
	LiveWidth              int       `json:"live_width,omitempty"`
	LiveHeight             int       `json:"live_height,omitempty"`
	FolderName             string    `json:"folder_name"`
	HasPassword            bool      `json:"has_password"`
	Enabled                bool      `json:"enabled"`
	Record                 bool      `json:"record"`
	SegmentDurationSeconds int64     `json:"segment_duration_seconds"`
	Upload                 bool      `json:"upload"`
	SFTPProfileID          string    `json:"sftp_profile_id,omitempty"`
	AudioCodec             string    `json:"audio_codec,omitempty"`
	AudioSampleRate        int       `json:"audio_sample_rate,omitempty"`
	AudioChannels          int       `json:"audio_channels,omitempty"`
	CreatedAt              time.Time `json:"created_at"`
	UpdatedAt              time.Time `json:"updated_at"`
}

func (c Camera) Public() CameraPublic {
	return CameraPublic{
		ID: c.ID, Name: c.Name, Host: c.Host, Username: c.Username, RTSPURL: RedactURL(c.RTSPURL), LiveRTSPURL: RedactURL(c.LiveRTSPURL),
		ProfileToken: c.ProfileToken, LiveProfileToken: c.LiveProfileToken, Manufacturer: c.Manufacturer, Model: c.Model,
		SerialNumber: c.SerialNumber, FirmwareVersion: c.FirmwareVersion, Codec: c.Codec, Width: c.Width, Height: c.Height,
		LiveCodec: c.LiveCodec, LiveWidth: c.LiveWidth, LiveHeight: c.LiveHeight, FolderName: c.FolderName, HasPassword: c.Password != "",
		Enabled: c.Enabled, Record: c.Record, SegmentDurationSeconds: c.SegmentDurationSeconds, Upload: c.Upload,
		SFTPProfileID: c.SFTPProfileID, AudioCodec: c.AudioCodec, AudioSampleRate: c.AudioSampleRate, AudioChannels: c.AudioChannels,
		CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
	}
}

type Session struct {
	ID        string    `json:"id"`
	CSRFToken string    `json:"csrf_token"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

type UploadJob struct {
	ID            string    `json:"id"`
	CameraID      string    `json:"camera_id"`
	SFTPProfileID string    `json:"sftp_profile_id,omitempty"`
	LocalPath     string    `json:"local_path"`
	RemotePath    string    `json:"remote_path"`
	SHA256        string    `json:"sha256,omitempty"`
	Size          int64     `json:"size"`
	Attempts      int       `json:"attempts"`
	NextAttempt   time.Time `json:"next_attempt"`
	CreatedAt     time.Time `json:"created_at"`
	CompletedAt   time.Time `json:"completed_at,omitempty"`
	LastError     string    `json:"last_error,omitempty"`
}

type UploadJobPublic struct {
	ID            string    `json:"id"`
	CameraID      string    `json:"camera_id"`
	SFTPProfileID string    `json:"sftp_profile_id,omitempty"`
	RemotePath    string    `json:"remote_path"`
	Size          int64     `json:"size"`
	Attempts      int       `json:"attempts"`
	NextAttempt   time.Time `json:"next_attempt"`
	CreatedAt     time.Time `json:"created_at"`
	LastError     string    `json:"last_error,omitempty"`
}

func (j UploadJob) Public() UploadJobPublic {
	return UploadJobPublic{
		ID: j.ID, CameraID: j.CameraID, SFTPProfileID: j.SFTPProfileID, RemotePath: j.RemotePath, Size: j.Size,
		Attempts: j.Attempts, NextAttempt: j.NextAttempt, CreatedAt: j.CreatedAt,
		LastError: RedactSecrets(j.LastError),
	}
}

type SFTPProfile struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Enabled        bool      `json:"enabled"`
	Host           string    `json:"host"`
	Port           int       `json:"port"`
	User           string    `json:"user"`
	Password       string    `json:"password,omitempty"`
	PrivateKeyPath string    `json:"private_key_path,omitempty"`
	KnownHostsPath string    `json:"known_hosts_path"`
	RemoteBaseDir  string    `json:"remote_base_dir"`
	DeleteLocal    bool      `json:"delete_local"`
	TimeoutSeconds int       `json:"timeout_seconds"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type SFTPProfilePublic struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Enabled        bool      `json:"enabled"`
	Host           string    `json:"host"`
	Port           int       `json:"port"`
	User           string    `json:"user"`
	HasPassword    bool      `json:"has_password"`
	PrivateKeyPath string    `json:"private_key_path,omitempty"`
	KnownHostsPath string    `json:"known_hosts_path"`
	RemoteBaseDir  string    `json:"remote_base_dir"`
	DeleteLocal    bool      `json:"delete_local"`
	TimeoutSeconds int       `json:"timeout_seconds"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func (p SFTPProfile) Public() SFTPProfilePublic {
	return SFTPProfilePublic{
		ID: p.ID, Name: p.Name, Enabled: p.Enabled, Host: p.Host, Port: p.Port, User: p.User, HasPassword: p.Password != "",
		PrivateKeyPath: p.PrivateKeyPath, KnownHostsPath: p.KnownHostsPath, RemoteBaseDir: p.RemoteBaseDir,
		DeleteLocal: p.DeleteLocal, TimeoutSeconds: p.TimeoutSeconds, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt,
	}
}

type RetentionPolicy struct {
	Enabled   bool      `json:"enabled"`
	Value     int       `json:"value"`
	Unit      string    `json:"unit"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (p RetentionPolicy) Cutoff(now time.Time) (time.Time, bool) {
	if !p.Enabled || p.Value < 1 {
		return time.Time{}, false
	}
	switch p.Unit {
	case "days":
		return now.AddDate(0, 0, -p.Value), true
	case "months":
		return now.AddDate(0, -p.Value, 0), true
	case "years":
		return now.AddDate(-p.Value, 0, 0), true
	default:
		return time.Time{}, false
	}
}

type RuntimeStatus struct {
	CameraID         string    `json:"camera_id"`
	State            string    `json:"state"`
	Codec            string    `json:"codec,omitempty"`
	LastError        string    `json:"last_error,omitempty"`
	ConnectedAt      time.Time `json:"connected_at,omitempty"`
	LastPacketAt     time.Time `json:"last_packet_at,omitempty"`
	RecordingPath    string    `json:"recording_path,omitempty"`
	RecordingStarted time.Time `json:"recording_started,omitempty"`
	BytesReceived    uint64    `json:"bytes_received"`
	PacketsReceived  uint64    `json:"packets_received"`
	Viewers          int       `json:"viewers"`
	LiveMode         string    `json:"live_mode,omitempty"`
	AudioCodec       string    `json:"audio_codec,omitempty"`
	AudioSampleRate  int       `json:"audio_sample_rate,omitempty"`
	AudioChannels    int       `json:"audio_channels,omitempty"`
}

type State struct {
	Version      int                    `json:"version"`
	Cameras      map[string]Camera      `json:"cameras"`
	Sessions     map[string]Session     `json:"sessions"`
	UploadQueue  map[string]UploadJob   `json:"upload_queue"`
	SFTPProfiles map[string]SFTPProfile `json:"sftp_profiles"`
	Retention    RetentionPolicy        `json:"retention"`
}

var urlPattern = regexp.MustCompile(`(?i)(?:rtsp|rtsps|http|https)://[^\s"'<>]+`)

func RedactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return raw
	}
	u.User = url.UserPassword("***", "***")
	return u.String()
}

func RedactSecrets(value string) string {
	return urlPattern.ReplaceAllStringFunc(value, func(candidate string) string {
		trimmed := strings.TrimRight(candidate, ".,;:)]}")
		suffix := candidate[len(trimmed):]
		return RedactURL(trimmed) + suffix
	})
}
