// Copyright (c) 2024 Chatsell. Fork addition — not part of upstream whatsmeow.
//
// This file adds catalog order-details fetching, kept alongside upstream's
// GetOrderDetails because it also parses the seller SKU (retailer_id).
// See GetCatalogOrderDetails for details.

package whatsmeow

import (
	"context"
	"strconv"

	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/types"
)

// OrderProduct is a single line item of a catalog order returned by GetCatalogOrderDetails.
type OrderProduct struct {
	ID         string  // WhatsApp internal product id.
	RetailerID string  // Seller-defined SKU (== product_retailer_id on the Cloud API, matches the synced e-commerce catalog).
	Name       string  // Product name as stored in the WhatsApp catalog.
	Quantity   int     // Units ordered.
	Price      float64 // Unit price, already divided by 1000 (WhatsApp sends prices as 1000-scaled integers, like OrderMessage.TotalAmount1000).
	Currency   string
}

// OrderDetails is the full detail of a catalog order ("Pedido enviado por catálogo").
type OrderDetails struct {
	Products      []OrderProduct
	TotalPrice    float64
	TotalCurrency string
}

// GetCatalogOrderDetails fetches the full line items of a catalog order.
//
// Upstream later added its own GetOrderDetails (business.go), but it does not
// parse retailer_id (the seller SKU we match against the synced catalog) and
// fails when creation_ts is missing, so the fork keeps this parser under a
// different name to avoid merge conflicts.
//
// The OrderMessage delivered over the multidevice (QR) socket only carries a
// summary — item count and total amount — never the individual products.
// WhatsApp Web resolves the line items with a separate IQ to the `fb:thrift_iq`
// namespace, using the order id + token that arrive inside the OrderMessage.
//
// The request shape is reverse-engineered from the WhatsApp Web client; the
// response tag names may drift. On any parse mismatch the raw response is logged
// at Debug (Node.String()) so the actual shape can be inspected and this parser
// adjusted. Callers should treat an error OR an empty Products slice as
// "fall back to the summary".
func (cli *Client) GetCatalogOrderDetails(ctx context.Context, orderID, token string) (*OrderDetails, error) {
	resp, err := cli.sendIQ(ctx, infoQuery{
		Namespace: "fb:thrift_iq",
		Type:      iqGet,
		To:        types.ServerJID,
		SMaxID:    "5",
		Content: []waBinary.Node{{
			Tag: "order",
			Attrs: waBinary.Attrs{
				"op": "get",
				"id": orderID,
			},
			Content: []waBinary.Node{
				{
					Tag: "image_dimensions",
					Content: []waBinary.Node{
						{Tag: "width", Content: []byte("100")},
						{Tag: "height", Content: []byte("100")},
					},
				},
				// The token authorizes reading this specific order's contents.
				{Tag: "token", Content: []byte(token)},
			},
		}},
	})
	if err != nil {
		return nil, err
	}

	orderNode, ok := resp.GetOptionalChildByTag("order")
	if !ok {
		cli.Log.Debugf("Order-details response had no <order> node, raw: %s", resp.String())
		return nil, &ElementMissingError{Tag: "order", In: "response to order details query"}
	}

	details := &OrderDetails{}
	for _, product := range orderNode.GetChildrenByTag("product") {
		p := OrderProduct{
			ID:         orderNodeChildString(&product, "id"),
			RetailerID: orderNodeChildString(&product, "retailer_id"),
			Name:       orderNodeChildString(&product, "name"),
			Currency:   orderNodeChildString(&product, "currency"),
		}
		if q, err := strconv.Atoi(orderNodeChildString(&product, "quantity")); err == nil && q > 0 {
			p.Quantity = q
		} else {
			p.Quantity = 1
		}
		if raw := orderNodeChildString(&product, "price"); raw != "" {
			if v, err := strconv.ParseFloat(raw, 64); err == nil {
				p.Price = v / 1000.0
			}
		}
		details.Products = append(details.Products, p)
	}

	if priceNode, ok := orderNode.GetOptionalChildByTag("price"); ok {
		details.TotalCurrency = orderNodeChildString(&priceNode, "currency")
		if raw := orderNodeChildString(&priceNode, "total"); raw != "" {
			if v, err := strconv.ParseFloat(raw, 64); err == nil {
				details.TotalPrice = v / 1000.0
			}
		}
	}

	if len(details.Products) == 0 {
		// Not an error: the query succeeded but our parser found nothing. Log the
		// raw shape so the tag names can be corrected without a live capture.
		cli.Log.Debugf("Order-details parsed 0 products, raw response: %s", resp.String())
	}

	return details, nil
}

// orderNodeChildString returns the string content of the first child with the
// given tag, or "" if it is missing or not a text/binary leaf.
func orderNodeChildString(n *waBinary.Node, tag string) string {
	child, ok := n.GetOptionalChildByTag(tag)
	if !ok {
		return ""
	}
	switch v := child.Content.(type) {
	case []byte:
		return string(v)
	case string:
		return v
	default:
		return ""
	}
}
