# Swap | the Crypto Swap Terminal

> Exchange crypto assets cross-chain leveraging multiple exchanges.

## Features

- multi-exchange support for best rates
- cross-chain swaps
- no account needed
- privacy-focused: your IP is not sent to exchanges
- simple navigation (esc go back / enter go forward)
- securely save/load your wallet address (sha256/hmac)
- search dozens of assets
- swap or pay (floating/fixed)
- QR code support
- open source
- no logs stored anywhere
- SSH-app (WIP: coming soon)

## Demo

<a href="https://swapcli.com/asset/demo.mp4">
  <img src="asset/demo.gif" alt="Demo GIF" />
</a>

_Click the image above to view the video_

## Installation

> download from [Github Releases](https://github.com/lfaoro/swap/releases) \
> OS [Linux, MacOS, Windows] \
> Arch [x86_64, arm64]

### Bash one-liner

```bash
curl -sSL https://get.swapcli.com | bash
```

### Docker

```bash
docker run -it lfaoro/swap
```

### Go install

```bash
go install github.com/lfaoro/swap/cmd/swap@latest
```

### MacOS (brew)

```bash
brew install lfaoro/tap/swap
```

### Nix (coming soon)

```bash
nix-env -iA swap
```

### Build

> requires [Go](https://go.dev/doc/install)

```bash
git clone https://github.com/lfaoro/swap.git \
  && cd swap \
  && make build \
  && bin/swap
```

## HTTP API Environment Variables

The `swap` backend supports a local HTTP API for quote providers. You can run a single provider or aggregate multiple providers concurrently (e.g. `0x,1inch,paraswap`).

- `SWAP_QUOTE_PROVIDER=0x|1inch|paraswap` — single provider mode.
- `SWAP_QUOTE_PROVIDER=0x,1inch,paraswap` — multi-provider aggregation mode (also supports `+`, `;`, or space-separated values).
- `SWAP_0X_API_KEY=<your-0x-api-key>` — required for 0x Permit2 quote endpoints.
- `SWAP_0X_CHAIN_ID=1|10|137|42161|56|8453|43114` — supported chain IDs. Default is `1`.
  - `1` → Ethereum Mainnet
  - `10` → Optimism
  - `137` → Polygon
  - `42161` → Arbitrum One
  - `56` → Binance Smart Chain (BSC)
  - `8453` → Base
  - `43114` → Avalanche C-Chain
- `SWAP_0X_TAKER=0x...` — valid Ethereum address for Permit2 quotes.
  - Default: `0x0000000000000000000000000000000000010000`
- `SWAP_PARASWAP_CHAIN_ID=1|10|137|42161|56` — supported chain IDs for ParaSwap.
  - Default: `1`
- `SWAP_PARASWAP_URL=<url>` — optional ParaSwap API base URL override.
  - Default: `https://apiv5.paraswap.io`
- `SWAP_COINS_SOURCE=coingecko|static` — source for `/v1/coins`.
  - Default: `coingecko` (dynamic list fetched from CoinGecko)
  - `static` uses the built-in fallback list
- `SWAP_COINS_LIMIT=1..250` — number of coins fetched from CoinGecko.
  - Default: `100`
- `SWAP_COINS_CACHE_TTL=<duration>` — in-memory cache TTL for coin list.
  - Default: `10m` (examples: `30s`, `5m`, `1h`)
- `chain_id` — optional request body field for `/v1/swaprate` that overrides `SWAP_0X_CHAIN_ID` when using 0x.
- `taker` — optional request body field for `/v1/swaprate` to override the `SWAP_0X_TAKER` address for Permit2 quotes. Must be a valid 0x-style address (0x... with 40 hex chars).

Polygon mapping note:

- On Polygon (`chain_id=137`), ticker `ETH` is mapped to Polygon WETH token (`0x7ceB23fD6bC0adD59E62ac25578270cFf1b9f619`) for quoting.

Supported token symbols for on-chain quote requests:

- `ETH`
- `USDC`
- `DAI`
- `WBTC`
- `USDT`

Example:

```bash
SWAP_QUOTE_PROVIDER=0x \
  SWAP_0X_API_KEY=your_key_here \
  SWAP_0X_CHAIN_ID=1 \
  SWAP_0X_TAKER=0x0000000000000000000000000000000000010000 \
  SWAP_COINS_SOURCE=coingecko \
  SWAP_COINS_LIMIT=100 \
  SWAP_COINS_CACHE_TTL=10m \
  go run ./cmd/httpapi
```

Multi-provider aggregation example:

```bash
SWAP_QUOTE_PROVIDER=0x,1inch,paraswap \
  SWAP_0X_API_KEY=your_key_here \
  SWAP_0X_CHAIN_ID=1 \
  SWAP_0X_TAKER=0x0000000000000000000000000000000000010000 \
  SWAP_PARASWAP_CHAIN_ID=1 \
  go run ./cmd/httpapi
```

## Storage

- GNU/Linux / MacOS `$HOME/.config/swap/config`
- Windows `%AppData%/swap/config`

## Remote Access

```bash
ssh swap@ssh.swapcli.com #(WIP: coming soon)
```

## Contributing

I love pull requests, don't hesitate.

## Exchanges

You want your exchange on Swapcli?
Send me an [email](exch@swapcli.com) with your API docs.

You want your exchange colorized in the list \
Submit a PR or donate and I will do it for you.

## Support

- [Telegram chat](https://t.me/swapcli)
- [GitHub issues](https://github.com/lfaoro/swap/issues)

## Show support

> If `swap` is useful to you, please consider giving it a ⭐.

- **star the repo**
- **tell your friends**

- [GitHub sponsor](https://github.com/sponsors/lfaoro)
- [BTC sponsor](https://mempool.space/address/bc1qzaqeqwklaq86uz8h2lww87qwfpnyh9fveyh3hs)
- [XMR sponsor](https://xmrchain.net/search?value=89XCyahmZiQgcVwjrSZTcJepPqCxZgMqwbABvzPKVpzC7gi8URDme8H6UThpCqX69y5i1aA81AKq57Wynjovy7g4K9MeY5c)
- [FIAT sponsor](https://checkout.revolut.com/pay/1122870b-1836-42e7-942b-90a99ef5e457)

## Roadmap

- [ ] implement auto clipboard
- [ ] implement birdpay feature
- [ ] create stylish themes
- [ ] add more exchanges

## How Swap works

- two coins and an amount has to be provided, then `swap` sends a request to all exchanges we have integrated and asks for an offer
- the ones that reply with an offer will be shown in the table sorted by `best offer`, their reputation rating (from A to E) and estimated time for completing the transaction
- select the best exchange by your criteria, input a receiving address and `swap` will create a transaction
- send the amount requested by the exchange and the converted amount is sent to your wallet

## Disclaimer

Swapcli.com provides a service (hereinafter referred to as "the Platform") that facilitates the swapping of crypto tokens by integrating third-party services. Please be aware of the following:

- **No Ownership or Custody**: I do not own, hold in custody, or control any of the crypto tokens that are exchanged. All transactions occur directly between users through external services.

- **Third-Party Services**: The Platform merely serves as an interface to external services for token transfers. I am not responsible for the execution, security, or performance of these third-party services.

- **Compliance**: While I strive to maintain compliance with EU regulations, users are personally responsible for adhering to local laws, including tax obligations, anti-money laundering (AML) regulations, and know your customer (KYC) requirements where applicable.

- **Risks Inherent in Crypto Transactions**: Cryptocurrency investments and transactions involve significant risks, such as price volatility, security breaches, and regulatory changes. Users should conduct thorough research and understand these risks before engaging with the Platform.

- **No Financial Advice**: The Platform does not provide financial, legal, or tax advice. Users should consult with professionals for such guidance.

- **Liability Limitation**: I disclaim any liability for any losses, damages, or costs arising from the use or inability to use the Platform, including but not limited to loss of profits, goodwill, or any other intangible losses, to the maximum extent permitted by law.

- **Commission**: A commission is charged on each transaction facilitated through the Platform, solely for the use of the interface. This does not imply involvement in the actual transfer of tokens.

- **Regulatory Changes**: The regulatory environment for cryptocurrencies is rapidly evolving, particularly with the implementation of the EU MiCA regulation. I will attempt to adapt to these changes, but cannot guarantee uninterrupted service or that all aspects of the service will remain compliant under new regulations.

- **Indemnification**: By using this Platform, you agree to indemnify, defend, and hold harmless all developers of this project from and against any claims, liabilities, damages, losses, and expenses, including legal fees, arising from your use of the services or your violation of this disclaimer.

- **Use at Your Own Risk**: All users engage with the Platform at their own risk.

**By using this service, you acknowledge that you have read, understood, and agreed to this disclaimer and the associated risks.**
