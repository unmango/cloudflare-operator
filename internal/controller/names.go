package controller

// Names of the pieces the Cloudflared controller manages inside the pod it creates.
const (
	cloudflaredContainerName = "cloudflared"
	configVolumeName         = "config"
)
