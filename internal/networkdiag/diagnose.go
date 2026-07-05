package networkdiag

import (
	"context"
	"net"
	"os"
	"sort"
	"strings"
	"time"

	fragrtsp "fragata/internal/rtsp"
)

type Report struct {
	Host            string               `json:"host"`
	InContainer     bool                 `json:"in_container"`
	LocalAddresses  []string             `json:"local_addresses"`
	SameLocalSubnet bool                 `json:"same_local_subnet"`
	PortChecks      []fragrtsp.PortCheck `json:"port_checks"`
	Summary         string               `json:"summary"`
	Recommendation  string               `json:"recommendation"`
}

func Diagnose(ctx context.Context, host string, ports []int, timeout time.Duration) Report {
	local, sameSubnet := localAddresses(host)
	checks := fragrtsp.CheckPorts(ctx, host, ports, timeout)
	report := Report{
		Host:            host,
		InContainer:     inContainer(),
		LocalAddresses:  local,
		SameLocalSubnet: sameSubnet,
		PortChecks:      checks,
	}
	report.Summary, report.Recommendation = explain(report)
	return report
}

func explain(report Report) (string, string) {
	if len(report.PortChecks) == 0 {
		return "No se ejecutaron comprobaciones de puerto.", "Revise la lista de puertos configurada para el diagnóstico."
	}
	states := make(map[string]int)
	for _, check := range report.PortChecks {
		states[check.State]++
		if check.Reachable {
			return "La cámara acepta conexiones TCP desde Fragata.", "La conectividad básica funciona; continúe con la prueba de URL RTSP y credenciales."
		}
	}
	if states["no_route"] > 0 {
		return "Fragata no tiene una ruta hacia la red de la cámara.", "Agregue una ruta, conecte el servidor a la misma LAN/VLAN o use una VPN que anuncie la subred de cámaras."
	}
	if states["refused"] == len(report.PortChecks) {
		return "La IP responde, pero los puertos probados están cerrados.", "Confirme la IP actual de la cámara, habilite RTSP/ONVIF y revise si el fabricante usa otro puerto."
	}
	if states["timeout"] == len(report.PortChecks) {
		if report.InContainer {
			return "La cámara no responde desde la red del contenedor.", "En Linux, ejecute Fragata con network_mode: host. Si continúa, revise firewall FORWARD, VLAN, aislamiento Wi-Fi o que la cámara siga usando esa IP."
		}
		if !report.SameLocalSubnet {
			return "La cámara no responde y Fragata no tiene una interfaz en la misma subred.", "Verifique la tabla de rutas o conecte el servidor a 192.168.10.0/24. Si está fuera del sitio, necesita una VPN o túnel de red; una URL RTSP no atraviesa Internet por sí sola."
		}
		return "La cámara no responde desde el servidor de Fragata.", "Revise la IP, el aislamiento entre clientes Wi-Fi, reglas VLAN/firewall y que RTSP esté habilitado."
	}
	return "No se pudo confirmar conectividad TCP con la cámara.", "Revise los estados de cada puerto. La ruta RTSP y la contraseña solo pueden validarse después de que al menos un puerto responda."
}

func localAddresses(target string) ([]string, bool) {
	targetIP := net.ParseIP(strings.Trim(target, "[]"))
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, false
	}
	var out []string
	sameSubnet := false
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addresses, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, address := range addresses {
			ip, network, err := net.ParseCIDR(address.String())
			if err != nil || ip == nil || ip.IsLoopback() {
				continue
			}
			out = append(out, address.String())
			if targetIP != nil && network.Contains(targetIP) {
				sameSubnet = true
			}
		}
	}
	sort.Strings(out)
	return out, sameSubnet
}

func inContainer() bool {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	raw, err := os.ReadFile("/proc/1/cgroup")
	if err != nil {
		return false
	}
	value := strings.ToLower(string(raw))
	return strings.Contains(value, "docker") || strings.Contains(value, "containerd") || strings.Contains(value, "kubepods")
}
