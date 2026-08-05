// spec/system/checkout_flow.spec.js
//
// Fixture: a `//` comment host, plus a trailing comma — normalized away as a
// courtesy alongside the three documented forms.

describe("expired-card checkout flow", () => {
  // @intent: { entity: "Order", action: "checkout", behavior: "shows a payment error card and keeps the cart intact when the saved card is expired", layer: "system", }
  it("keeps the cart intact", async () => {
    await page.click("#checkout");
    await expect(page.locator(".cart")).toHaveCount(1);
  });
});
