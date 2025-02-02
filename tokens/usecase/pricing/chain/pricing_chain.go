package chainpricing

import (
	"context"
	"fmt"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/osmosis-labs/osmosis/osmomath"
	"github.com/osmosis-labs/sqs/domain"
	"github.com/osmosis-labs/sqs/domain/cache"
	"github.com/osmosis-labs/sqs/domain/mvc"
)

type chainPricing struct {
	TUsecase mvc.TokensUsecase
	RUsecase mvc.RouterUsecase

	cache         *cache.Cache
	cacheExpiryNs time.Duration

	defaultQuoteDenom string

	maxPoolsPerRoute int
	maxRoutes        int
	minOSMOLiquidity int
}

var _ domain.PricingSource = &chainPricing{}

const (
	// We use multiplier so that stablecoin quotes avoid selecting low liquidity routes.
	// USDC/USDT value of 10 should be sufficient to avoid low liquidity routes.
	tokenInMultiplier = 10
)

var (
	cacheHitsCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "sqs_pricing_cache_hits_total",
			Help: "Total number of pricing cache hits",
		},
		[]string{"base", "quote"},
	)
	cacheMissesCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "sqs_pricing_cache_misses_total",
			Help: "Total number of pricing cache misses",
		},
		[]string{"base", "quote"},
	)

	pricesTruncationCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "sqs_pricing_truncation_total",
			Help: "Total number of price truncations in intermediary calculations",
		},
		[]string{"base", "quote"},
	)

	pricesSpotPriceError = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "sqs_pricing_spot_price_error_total",
			Help: "Total number of spot price errors in pricing",
		},
		[]string{"base", "quote"},
	)
)

func init() {
	prometheus.MustRegister(cacheHitsCounter)
	prometheus.MustRegister(cacheMissesCounter)
}

func New(routerUseCase mvc.RouterUsecase, tokenUseCase mvc.TokensUsecase, config domain.PricingConfig) domain.PricingSource {
	chainDefaultHumanDenom, err := tokenUseCase.GetChainDenom(config.DefaultQuoteHumanDenom)
	if err != nil {
		panic(fmt.Sprintf("failed to get chain denom for default quote human denom (%s): %s", config.DefaultQuoteHumanDenom, err))
	}

	return &chainPricing{
		RUsecase: routerUseCase,
		TUsecase: tokenUseCase,

		cache:             cache.New(),
		cacheExpiryNs:     time.Duration(config.CacheExpiryMs) * time.Millisecond,
		maxPoolsPerRoute:  config.MaxPoolsPerRoute,
		maxRoutes:         config.MaxRoutes,
		minOSMOLiquidity:  config.MinOSMOLiquidity,
		defaultQuoteDenom: chainDefaultHumanDenom,
	}
}

// GetPrice implements pricing.PricingStrategy.
func (c *chainPricing) GetPrice(ctx context.Context, baseDenom string, quoteDenom string, opts ...domain.PricingOption) (osmomath.BigDec, error) {
	options := domain.PricingOptions{
		MinLiquidity: c.minOSMOLiquidity,
	}

	for _, opt := range opts {
		opt(&options)
	}

	// Recompute prices if desired by configuration.
	// Otherwise, look into cache first.
	if options.RecomputePrices {
		return c.computePrice(ctx, baseDenom, quoteDenom, options.MinLiquidity)
	}

	// equal base and quote yield the price of one
	if baseDenom == quoteDenom {
		return osmomath.OneBigDec(), nil
	}

	cacheKey := domain.FormatPricingCacheKey(baseDenom, quoteDenom)

	cachedValue, found := c.cache.Get(cacheKey)
	if found {
		// Cast cached value to correct type.
		cachedBigDecPrice, ok := cachedValue.(osmomath.BigDec)
		if !ok {
			return osmomath.BigDec{}, fmt.Errorf("invalid type cached in pricing, expected BigDec, got (%T)", cachedValue)
		}

		// Increase cache hits
		cacheHitsCounter.WithLabelValues(baseDenom, quoteDenom).Inc()
		return cachedBigDecPrice, nil
	} else if !found {
		// Increase cache misses
		cacheMissesCounter.WithLabelValues(baseDenom, quoteDenom).Inc()
	}

	// If cache miss occurs, we compute the price.
	return c.computePrice(ctx, baseDenom, quoteDenom, options.MinLiquidity)
}

// computePrice computes the price for a given base and quote denom
func (c *chainPricing) computePrice(ctx context.Context, baseDenom string, quoteDenom string, minLiquidity int) (osmomath.BigDec, error) {
	cacheKey := domain.FormatPricingCacheKey(baseDenom, quoteDenom)

	if baseDenom == quoteDenom {
		return osmomath.OneBigDec(), nil
	}

	// Get on-chain scaling factor for base denom.
	baseDenomScalingFactor, err := c.TUsecase.GetChainScalingFactorByDenomMut(baseDenom)
	if err != nil {
		return osmomath.BigDec{}, err
	}

	// Get on-chain scaling factor for quote denom.
	quoteDenomScalingFactor, err := c.TUsecase.GetChainScalingFactorByDenomMut(quoteDenom)
	if err != nil {
		return osmomath.BigDec{}, err
	}

	// Create a quote denom coin.
	// We use multiplier so that stablecoin quotes avoid selecting low liquidity routes.
	tenQuoteCoin := sdk.NewCoin(quoteDenom, osmomath.NewInt(tokenInMultiplier).Mul(quoteDenomScalingFactor.TruncateInt()))

	// Overwrite default config with custom values
	// necessary for pricing.
	routingOptions := []domain.RouterOption{
		domain.WithMaxRoutes(c.maxRoutes),
		domain.WithMaxPoolsPerRoute(c.maxPoolsPerRoute),
		// Use the provided min liquidity value rather than the default
		// Since it can be overridden by options in GetPrice(...)
		domain.WithMinOSMOLiquidity(minLiquidity),
		domain.WithDisableSplitRoutes(),
	}

	// Compute a quote for one quote coin.
	quote, err := c.RUsecase.GetOptimalQuote(ctx, tenQuoteCoin, baseDenom, routingOptions...)
	if err != nil {
		return osmomath.BigDec{}, err
	}
	if quote == nil {
		return osmomath.BigDec{}, fmt.Errorf("no quote found when computing pricing for %s (base) -> %s (quote)", baseDenom, quoteDenom)
	}

	routes := quote.GetRoute()
	if len(routes) == 0 {
		return osmomath.BigDec{}, fmt.Errorf("no route found when computing pricing for %s (base) -> %s (quote)", baseDenom, quoteDenom)
	}

	route := routes[0]

	chainPrice := osmomath.OneBigDec()

	pools := route.GetPools()

	var (
		tempQuoteDenom       = quoteDenom
		tempBaseDenom        string
		useAlternativeMethod = false
	)

	// If Astroport pool, use the alternative meth

	for _, pool := range pools {
		tempBaseDenom = pool.GetTokenOutDenom()

		// Get spot price for the pool.
		poolSpotPrice, err := c.RUsecase.GetPoolSpotPrice(ctx, pool.GetId(), tempQuoteDenom, tempBaseDenom)
		if err != nil || poolSpotPrice.IsNil() || poolSpotPrice.IsZero() {
			// Increase price truncation counter
			pricesSpotPriceError.WithLabelValues(baseDenom, quoteDenom).Inc()

			useAlternativeMethod = true
			break
		}

		// Multiply spot price by the previous spot price.
		chainPrice = chainPrice.MulMut(poolSpotPrice)

		tempQuoteDenom = tempBaseDenom
	}

	if useAlternativeMethod {
		// Compute on-chain price for 1 unit of base denom and quote denom.
		chainPrice = osmomath.NewBigDecFromBigInt(tenQuoteCoin.Amount.BigIntMut()).QuoMut(osmomath.NewBigDecFromBigInt(quote.GetAmountOut().BigIntMut()))
	}

	if chainPrice.IsZero() {
		// Increase price truncation counter
		pricesTruncationCounter.WithLabelValues(baseDenom, quoteDenom).Inc()
	}

	// Compute precision scaling factor.
	precisionScalingFactor := osmomath.BigDecFromDec(osmomath.NewDec(tokenInMultiplier).MulMut(baseDenomScalingFactor.Quo(tenQuoteCoin.Amount.ToLegacyDec())))

	// Apply scaling facors to descale the amounts to real amounts.
	currentPrice := chainPrice.MulMut(precisionScalingFactor)

	// Only store values that are valid.
	if !currentPrice.IsNil() {
		expirationTTL := c.cacheExpiryNs
		// We pre-compute the price for the default quote denom in ingest handler via the background
		// pricing worker. As a result, we store them indefinitely.
		// We track the tokens that are modified within the block and update the prices only for those tokens.
		if quoteDenom == c.defaultQuoteDenom {
			expirationTTL = cache.NoExpirationTTL
		}
		c.cache.Set(cacheKey, currentPrice, expirationTTL)
	}

	return currentPrice, nil
}

// InitializeCache implements domain.PricingSource.
func (c *chainPricing) InitializeCache(cache *cache.Cache) {
	c.cache = cache
}
