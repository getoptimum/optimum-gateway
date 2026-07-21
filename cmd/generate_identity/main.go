package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/getoptimum/optimum-common/pkg/identity"
)

func main() {
	pwd, err := os.Getwd()
	if err != nil {
		log.Fatal("unable to get working directory", err)
	}

	if _, err = identity.EnsureIdentity(pwd); err != nil {
		log.Fatal("unable to generate identity", err)
	}
	key, err := identity.ExtractIdentityFromDir(pwd)
	if err != nil {
		log.Fatal("unable to extract identity", err)
	}
	log.Printf("Identity generated successfully. Public Key: %s\n", key.ID.String())
	log.Printf("local peer multiaddress: /ip4/127.0.0.1/tcp/13844/p2p/%s\n", key.ID.String())

	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Fatal("unable to get user home directory", err)
	}

	jwtPath := filepath.Join(homeDir, "local_cl", "local_eth", "jwt")
	prysmPath := filepath.Join(homeDir, "local_cl", ".prysm_data")
	gethPath := filepath.Join(homeDir, "local_cl", ".geth_data")
	gatewayPath := filepath.Join(homeDir, "local_cl", ".gateway_data", "libp2p")

	targetFolders := []string{
		jwtPath,
		prysmPath,
		gethPath,
		gatewayPath,
	}
	for _, folder := range targetFolders {
		if err = os.MkdirAll(folder, 0o700); err != nil {
			log.Fatal("unable to create directory", err, folder)
		}
	}

	// move files to optimum identity folder
	targetPath := filepath.Join(gatewayPath, "p2p.key")
	if err = os.Rename(filepath.Join(pwd, "p2p.key"), targetPath); err != nil {
		log.Fatal("unable to move private_key file", err)
	}
	log.Printf("file location: %s\n", targetPath)

	envFile := filepath.Join(pwd, ".env")
	envVariables := []string{
		fmt.Sprintf("ETH_JWT_DIR=%s", jwtPath),
		fmt.Sprintf("PRYSM_DATA_DIR=%s", prysmPath),
		fmt.Sprintf("GET_DATA_DIR=%s", gethPath),
		fmt.Sprintf("GATEWAY_PEER_ID=%s", key.ID.String()),
		fmt.Sprintf("GATEWAY_PEER=/ip4/127.0.0.1/tcp/13844/p2p/%s", key.ID.String()),
	}
	if err = os.WriteFile(envFile, []byte(strings.Join(envVariables, "\n")), 0o600); err != nil {
		log.Fatal("unable to create .env file", err)
	}
	log.Printf(".env file created/updated at %s\n", envFile)
}
