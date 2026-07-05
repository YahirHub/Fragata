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
	Enabled                bool      `json:"enabled"`
	Record                 bool      `json:"record"`
	SegmentDurationSeconds int64     `json:"segment_duration_seconds"`
	Upload                 bool      `json:"upload"`
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
	Enabled                bool      `json:"enabled"`
	Record                 bool      `json:"record"`
	SegmentDurationSeconds int64     `json:"segment_duration_seconds"`
	Upload                 bool      `json:"upload"`
	CreatedAt              time.Time `json:"created_at"`
	UpdatedAt              time.Time `json:"updated_at"`
}

func (c Camera) Public() CameraPublic {
	return CameraPublic{
		ID: c.ID, Name: c.Name, Host: c.Host, Username: c.Username, RTSPURL: RedactURL(c.RTSPURL), LiveRTSPURL: RedactURL(c.LiveRTSPURL),
		ProfileToken: c.ProfileToken, LiveProfileToken: c.LiveProfileToken, Manufacturer: c.Manufacturer, Model: c.Model,
		SerialNumber: c.SerialNumber, FirmwareVersion: c.FirmwareVersion, Codec: c.Codec, Width: c.Width, Height: c.Height,
		LiveCodec: c.LiveCodec, LiveWidth: c.LiveWidth, LiveHeight: c.LiveHeight,
		Enabled: c.Enabled, Record: c.Record, SegmentDurationSeconds: c.SegmentDurationSeconds, Upload: c.Upload,
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
	ID          string    `json:"id"`
	CameraID    string    `json:"camera_id"`
	LocalPath   string    `json:"local_path"`
	RemotePath  string    `json:"remote_path"`
	SHA256      string    `json:"sha256,omitempty"`
	Size        int64     `json:"size"`
	Attempts    int       `json:"attempts"`
	NextAttempt time.Time `json:"next_attempt"`
	CreatedAt   time.Time `json:"created_at"`
	CompletedAt time.Time `json:"completed_at,omitempty"`
	LastError   string    `json:"last_error,omitempty"`
}

type UploadJobPublic struct {
	ID          string    `json:"id"`
	CameraID    string    `json:"camera_id"`
	RemotePath  string    `json:"remote_path"`
	Size        int64     `json:"size"`
	Attempts    int       `json:"attempts"`
	NextAttempt time.Time `json:"next_attempt"`
	CreatedAt   time.Time `json:"created_at"`
	LastError   string    `json:"last_error,omitempty"`
}

func (j UploadJob) Public() UploadJobPublic {
	return UploadJobPublic{
		ID: j.ID, CameraID: j.CameraID, RemotePath: j.RemotePath, Size: j.Size,
		Attempts: j.Attempts, NextAttempt: j.NextAttempt, CreatedAt: j.CreatedAt,
		LastError: RedactSecrets(j.LastError),
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
}

type State struct {
	Version     int                  `json:"version"`
	Cameras     map[string]Camera    `json:"cameras"`
	Sessions    map[string]Session   `json:"sessions"`
	UploadQueue map[string]UploadJob `json:"upload_queue"`
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
