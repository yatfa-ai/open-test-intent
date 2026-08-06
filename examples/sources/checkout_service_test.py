# tests/services/test_checkout_service.py
#
# Fixture: the annotation is language-agnostic. Nothing in the extractor knows
# about Ruby — it looks for the `@intent` marker and the object literal that
# follows it, whatever comment syntax the host language uses.

import pytest

from billing.checkout_service import CheckoutService


# @intent: { entity: 'CheckoutService', action: 'checkout', behavior: 'rejects the transaction on an expired card and emits a payment_failed event', layer: 'unit', preconditions: ['card is on file', 'card is expired'] }
def test_rejects_expired_card(expired_card):
    with pytest.raises(CheckoutService.PaymentDeclined):
        CheckoutService(card=expired_card).checkout()


# @intent: {entity:"CheckoutService",action:"apply_discount",behavior:"applies the active percentage discount before tax is calculated",layer:"unit"}
def test_applies_discount_before_tax(order):
    assert CheckoutService(order=order).apply_discount().tax_base == 90
