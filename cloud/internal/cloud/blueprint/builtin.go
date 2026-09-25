package blueprint

import (
	"strings"

	v1 "github.com/athNdev/carbon-panel/pkg/proto/cloud/v1"
)

// BuiltinBlueprints returns the global list of builtin server flavors.
func BuiltinBlueprints() []*v1.Blueprint {
	return []*v1.Blueprint{
		{
			Id:                   "vanilla",
			Name:                 "Minecraft Vanilla",
			Description:          "Official Mojang dedicated server. Pure unmodded Minecraft experience.",
			Loader:               "vanilla",
			MinecraftVersion:     "1.21.4",
			DockerImage:          "itzg/minecraft-server:latest",
			DefaultMemoryMb:      2048,
			DefaultCpuMillicores: 1000,
			DefaultEnv: map[string]string{
				"EULA": "TRUE",
				"TYPE": "VANILLA",
			},
			Builtin: true,
		},
		{
			Id:                   "paper",
			Name:                 "PaperMC",
			Description:          "High-performance Minecraft server software aiming to fix gameplay and mechanics inconsistencies with Bukkit/Spigot plugins.",
			Loader:               "paper",
			MinecraftVersion:     "1.21.4",
			DockerImage:          "itzg/minecraft-server:latest",
			DefaultMemoryMb:      4096,
			DefaultCpuMillicores: 2000,
			DefaultEnv: map[string]string{
				"EULA": "TRUE",
				"TYPE": "PAPER",
			},
			DefaultJvmFlags: []string{
				"-XX:+UseG1GC",
				"-XX:+ParallelRefProcEnabled",
				"-XX:MaxGCPauseMillis=200",
			},
			Builtin: true,
		},
		{
			Id:                   "purpur",
			Name:                 "Purpur",
			Description:          "Drop-in replacement for Paper servers designed for configurability and new fun gameplay features.",
			Loader:               "purpur",
			MinecraftVersion:     "1.21.4",
			DockerImage:          "itzg/minecraft-server:latest",
			DefaultMemoryMb:      4096,
			DefaultCpuMillicores: 2000,
			DefaultEnv: map[string]string{
				"EULA": "TRUE",
				"TYPE": "PURPUR",
			},
			DefaultJvmFlags: []string{
				"-XX:+UseG1GC",
				"-XX:+ParallelRefProcEnabled",
				"-XX:MaxGCPauseMillis=200",
			},
			Builtin: true,
		},
		{
			Id:                   "fabric",
			Name:                 "Fabric",
			Description:          "Lightweight, modular modding toolchain for Minecraft. Ideal for modern client & server optimization mods.",
			Loader:               "fabric",
			MinecraftVersion:     "1.21.4",
			DockerImage:          "itzg/minecraft-server:latest",
			DefaultMemoryMb:      4096,
			DefaultCpuMillicores: 2000,
			DefaultEnv: map[string]string{
				"EULA": "TRUE",
				"TYPE": "FABRIC",
			},
			Builtin: true,
		},
		{
			Id:                   "forge",
			Name:                 "Minecraft Forge",
			Description:          "The classic modding platform for large technical, magic, and adventure modpacks.",
			Loader:               "forge",
			MinecraftVersion:     "1.20.1",
			DockerImage:          "itzg/minecraft-server:latest",
			DefaultMemoryMb:      6144,
			DefaultCpuMillicores: 3000,
			DefaultEnv: map[string]string{
				"EULA": "TRUE",
				"TYPE": "FORGE",
			},
			Builtin: true,
		},
		{
			Id:                   "neoforge",
			Name:                 "NeoForge",
			Description:          "Community-driven continuation and modern fork of Minecraft Forge for 1.20.2+.",
			Loader:               "neoforge",
			MinecraftVersion:     "1.21.4",
			DockerImage:          "itzg/minecraft-server:latest",
			DefaultMemoryMb:      6144,
			DefaultCpuMillicores: 3000,
			DefaultEnv: map[string]string{
				"EULA": "TRUE",
				"TYPE": "NEOFORGE",
			},
			Builtin: true,
		},
		{
			Id:                   "velocity",
			Name:                 "Velocity Proxy",
			Description:          "Next-generation, highly performant Minecraft server proxy. Connects multiple backend servers under a single IP.",
			Loader:               "velocity",
			MinecraftVersion:     "3.3.0",
			DockerImage:          "itzg/minecraft-server:latest",
			DefaultMemoryMb:      1024,
			DefaultCpuMillicores: 1000,
			DefaultEnv: map[string]string{
				"EULA": "TRUE",
				"TYPE": "VELOCITY",
			},
			Builtin: true,
		},
		{
			Id:                   "bedrock",
			Name:                 "Minecraft Bedrock",
			Description:          "Official dedicated server for Minecraft Bedrock edition (iOS, Android, Xbox, PlayStation, Windows 10/11).",
			Loader:               "bedrock",
			MinecraftVersion:     "LATEST",
			DockerImage:          "itzg/minecraft-bedrock-server:latest",
			DefaultMemoryMb:      2048,
			DefaultCpuMillicores: 1000,
			DefaultEnv: map[string]string{
				"EULA":       "TRUE",
				"GAMEMODE":   "survival",
				"DIFFICULTY": "normal",
			},
			Builtin: true,
		},
	}
}

// FindBuiltin searches builtins by ID (case-insensitive).
func FindBuiltin(id string) *v1.Blueprint {
	clean := strings.ToLower(strings.TrimSpace(id))
	for _, b := range BuiltinBlueprints() {
		if strings.ToLower(b.Id) == clean || strings.ToLower(b.Loader) == clean {
			// Return a copy so caller mutations don't affect standard catalog
			cp := *b
			if b.DefaultEnv != nil {
				cp.DefaultEnv = make(map[string]string, len(b.DefaultEnv))
				for k, v := range b.DefaultEnv {
					cp.DefaultEnv[k] = v
				}
			}
			if len(b.DefaultJvmFlags) > 0 {
				cp.DefaultJvmFlags = append([]string(nil), b.DefaultJvmFlags...)
			}
			return &cp
		}
	}
	return nil
}
