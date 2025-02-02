package domain

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/osmosis-labs/sqs/sqsdomain"

	"github.com/osmosis-labs/osmosis/osmomath"
)

type RoutableResultPool interface {
	sqsdomain.RoutablePool
	GetBalances() sdk.Coins
}

type Route interface {
	// ContainsGeneralizedCosmWasmPool returns true if the route contains a generalized cosmwasm pool.
	// We track whether a route contains a generalized cosmwasm pool
	// so that we can exclude it from split quote logic.
	// The reason for this is that making network requests to chain is expensive.
	// As a result, we want to minimize the number of requests we make.
	ContainsGeneralizedCosmWasmPool() bool
	GetPools() []sqsdomain.RoutablePool
	// CalculateTokenOutByTokenIn calculates the token out amount given the token in amount.
	// Returns error if the calculation fails.
	CalculateTokenOutByTokenIn(ctx context.Context, tokenIn sdk.Coin) (sdk.Coin, error)

	GetTokenOutDenom() string

	// PrepareResultPools strips away unnecessary fields
	// from each pool in the route,
	// leaving only the data needed by client
	// Runs the quote logic one final time to compute the effective spot price.
	// Note that it mutates the route.
	// Computes the spot price of the route.
	// Returns the spot price before swap and effective spot price.
	// The token in is the base token and the token out is the quote token.
	PrepareResultPools(ctx context.Context, tokenIn sdk.Coin) ([]sqsdomain.RoutablePool, osmomath.Dec, osmomath.Dec, error)

	String() string
}

type SplitRoute interface {
	Route
	GetAmountIn() osmomath.Int
	GetAmountOut() osmomath.Int
}

type Quote interface {
	GetAmountIn() sdk.Coin
	GetAmountOut() osmomath.Int
	GetRoute() []SplitRoute
	GetEffectiveSpreadFactor() osmomath.Dec
	GetPriceImpact() osmomath.Dec

	// PrepareResult mutates the quote to prepare
	// it with the data formatted for output to the client.
	// scalingFactor is the spot price scaling factor according to chain precision.
	// scalingFactor of zero is a valid value. It might occur if we do not have precision information
	// for the tokens. In that case, we invalidate spot price by setting it to zero.
	PrepareResult(ctx context.Context, scalingFactor osmomath.Dec) ([]SplitRoute, osmomath.Dec, error)

	String() string
}

type RouterConfig struct {
	PreferredPoolIDs   []uint64 `mapstructure:"preferred-pool-ids"`
	MaxPoolsPerRoute   int      `mapstructure:"max-pools-per-route"`
	MaxRoutes          int      `mapstructure:"max-routes"`
	MaxSplitRoutes     int      `mapstructure:"max-split-routes"`
	MaxSplitIterations int      `mapstructure:"max-split-iterations"`
	// Denominated in OSMO (not uosmo)
	MinOSMOLiquidity          int  `mapstructure:"min-osmo-liquidity"`
	RouteUpdateHeightInterval int  `mapstructure:"route-update-height-interval"`
	RouteCacheEnabled         bool `mapstructure:"route-cache-enabled"`
	// The number of milliseconds to cache candidate routes for before expiry.
	CandidateRouteCacheExpirySeconds int `mapstructure:"candidate-route-cache-expiry-seconds"`
	RankedRouteCacheExpirySeconds    int `mapstructure:"ranked-route-cache-expiry-seconds"`
	// Flag indicating whether we should have a cache for overwrite routes enabled.
	EnableOverwriteRoutesCache bool `mapstructure:"enable-overwrite-routes-cache"`
}

type PoolsConfig struct {
	TransmuterCodeIDs      []uint64 `mapstructure:"transmuter-code-ids"`
	GeneralCosmWasmCodeIDs []uint64 `mapstructure:"general-cosmwasm-code-ids"`
}

const DisableSplitRoutes = 0

type RouterState struct {
	Pools     []sqsdomain.PoolI
	TakerFees sqsdomain.TakerFeeMap
	TickMap   map[uint64]*sqsdomain.TickModel
}

// RouterOptions defines the options for the router
// By default, the router config that is defined on the router usecase is set.
// The caller of GetQuote(...) may overwrite the config with the options provided here.
// This is useful for pricing where we may want to use different parameters than the default config.
// With pricing, it is desired to use more pools with lower min liquidity parameter.
type RouterOptions struct {
	MaxPoolsPerRoute   int
	MaxRoutes          int
	MaxSplitRoutes     int
	MaxSplitIterations int
	// Denominated in OSMO (not uosmo)
	MinOSMOLiquidity int
	// The number of milliseconds to cache candidate routes for before expiry.
	CandidateRouteCacheExpirySeconds int
	RankedRouteCacheExpirySeconds    int
}

// DefaultRouterOptions defines the default options for the router
var DefaultRouterOptions = RouterOptions{}

// RouterOption configures the router options.
type RouterOption func(*RouterOptions)

// WithMinOSMOLiquidity configures the router options with the min OSMO liquidity.
func WithMinOSMOLiquidity(minOSMOLiquidity int) RouterOption {
	return func(o *RouterOptions) {
		o.MinOSMOLiquidity = minOSMOLiquidity
	}
}

// WithMaxPoolsPerRoute configures the router options with the max pools per route.
func WithMaxPoolsPerRoute(maxPoolsPerRoute int) RouterOption {
	return func(o *RouterOptions) {
		o.MaxPoolsPerRoute = maxPoolsPerRoute
	}
}

// WithMaxRoutes configures the router options with the max routes.
func WithMaxRoutes(maxRoutes int) RouterOption {
	return func(o *RouterOptions) {
		o.MaxRoutes = maxRoutes
	}
}

// WithDisableSplitRoutes configures the router options with the disabled split routes.
func WithDisableSplitRoutes() RouterOption {
	return WithMaxSplitRoutes(DisableSplitRoutes)
}

// WithMaxSplitRoutes configures the router options with the max split routes.
func WithMaxSplitRoutes(maxSplitRoutes int) RouterOption {
	return func(o *RouterOptions) {
		o.MaxSplitRoutes = maxSplitRoutes
	}
}
