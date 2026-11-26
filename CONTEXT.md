# Context: Optimum Gateway

Relays beacon blocks and attestations between an Ethereum consensus client and the mump2p network.

## Glossary

### Cluster
A mump2p cluster, identified by `ClusterID`. Gateways in the same cluster exchange blocks over mump2p.
Not a **Turbo gateway cluster**.

### Turbo gateway cluster
A regional group of gateways that each run a **Turbonode** beside them, for example Europe, US, Tokyo or Asia.
Membership is chosen by hand and may grow or shrink. Not a mump2p **Cluster**.

### Turbo gateway
A gateway that belongs to a **Turbo gateway cluster** and runs a **Turbonode**
beside it. It keeps its usual gateway type; it is not a separate gateway type.

### Turbonode
An internal Optimum process that runs in the same image as one gateway and connects that gateway to **Turbonet**.

### Turbonet
The network that links **Turbo gateway clusters** in different regions.
