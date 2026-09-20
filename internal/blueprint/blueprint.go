package blueprint

import (
	"context"

	storage "github.com/athNdev/carbon-panel/internal/db"
)

// Seed returns the builtin server blueprints (MINE-143).
func Seed() []storage.ServerBlueprint {
	env := func(s string) string { return s }
	return []storage.ServerBlueprint{
		{
			ID: "builtin-vanilla", Name: "Vanilla", Description: "Unmodified Mojang server. Best for pure survival.",
			ModLoader: storage.ModLoaderVanilla, MCVersion: "LATEST",
			DefaultEnv: env(`{"EULA":"TRUE","TYPE":"VANILLA","MEMORY":"2G"}`), Builtin: true,
		},
		{
			ID: "builtin-paper", Name: "Paper", Description: "High-performance Paper server with plugin support.",
			ModLoader: storage.ModLoaderPaper, MCVersion: "LATEST",
			DefaultEnv: env(`{"EULA":"TRUE","TYPE":"PAPER","MEMORY":"4G"}`), Builtin: true,
		},
		{
			ID: "builtin-fabric", Name: "Fabric", Description: "Fabric mod loader for lightweight modded servers.",
			ModLoader: storage.ModLoaderFabric, MCVersion: "LATEST",
			DefaultEnv: env(`{"EULA":"TRUE","TYPE":"FABRIC","MEMORY":"4G"}`), Builtin: true,
		},
		{
			ID: "builtin-forge", Name: "Forge", Description: "Forge mod loader for large modpacks.",
			ModLoader: storage.ModLoaderForge, MCVersion: "LATEST",
			DefaultEnv: env(`{"EULA":"TRUE","TYPE":"FORGE","MEMORY":"6G"}`), Builtin: true,
		},
		{
			ID: "builtin-neoforge", Name: "NeoForge", Description: "NeoForge mod loader (1.20.2+ Forge successor).",
			ModLoader: storage.ModLoaderNeoForge, MCVersion: "LATEST",
			DefaultEnv: env(`{"EULA":"TRUE","TYPE":"NEOFORGE","MEMORY":"6G"}`), Builtin: true,
		},
		{
			ID: "builtin-bedrock", Name: "Bedrock", Description: "Bedrock dedicated server for console/mobile cross-play.",
			ModLoader: storage.ModLoaderVanilla, MCVersion: "LATEST",
			DockerImage: "itzg/minecraft-bedrock-server:latest",
			DefaultEnv:  env(`{"EULA":"TRUE","GAMEMODE":"survival","DIFFICULTY":"normal"}`), Builtin: true,
		},
	}
}

// InitBuiltinBlueprints upserts builtin seeds (idempotent).
func InitBuiltinBlueprints(store *storage.Store) error {
	ctx := context.Background()
	for _, b := range Seed() {
		bp := b
		if err := store.SaveBlueprint(ctx, &bp); err != nil {
			return err
		}
	}
	return nil
}
