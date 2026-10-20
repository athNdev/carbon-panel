package module

import "testing"

func TestGeyserBedrockStaysPublic(t *testing.T) {
	port := geyserBedrockPort()
	if !port.ExposePublic {
		t.Fatal("Geyser Bedrock UDP must be marked public")
	}
	if port.Protocol != "udp" || port.ContainerPort != 19132 {
		t.Fatalf("port = %+v", port)
	}
}
