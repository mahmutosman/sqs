#!/bin/bash

url=$1

# This script compares single hop quotes by running them against SQS and chain directly.

chain_amount_out=$(osmosisd q poolmanager estimate-swap-exact-amount-in 1436 100000000factory/osmo1z0qrq605sjgcqpylfl4aa6s90x738j7m58wyatt0tdzflg2ha26q67k743/wbtc --swap-route-pool-ids 1436 --swap-route-denoms ibc/498A0751C798A0D9A389AA3691123DADA57DAA4FE165D5C75894505B876BA6E4 --node $url:26657)

sqs_custom_res=$(curl "$url:9092/router/custom-direct-quote?tokenIn=100000000factory/osmo1z0qrq605sjgcqpylfl4aa6s90x738j7m58wyatt0tdzflg2ha26q67k743/wbtc&tokenOutDenom=ibc/498A0751C798A0D9A389AA3691123DADA57DAA4FE165D5C75894505B876BA6E4&poolID=1436")
sqs_custom_amount_out=$(echo $sqs_custom_res | jq .amount_out)

sqs_optimal_res=$(curl "$url:9092/router/quote?tokenIn=100000000factory/osmo1z0qrq605sjgcqpylfl4aa6s90x738j7m58wyatt0tdzflg2ha26q67k743/wbtc&tokenOutDenom=ibc/498A0751C798A0D9A389AA3691123DADA57DAA4FE165D5C75894505B876BA6E4")
sqs_optimal_amount_out=$(echo $sqs_optimal_res | jq .amount_out)

echo "chain_amount_out: $chain_amount_out"
echo "sqs_custom_amount_out: $sqs_custom_amount_out"
echo "sqs_optimal_amount_out: $sqs_optimal_amount_out"
