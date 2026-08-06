# Reached by `--source 'spec/**/*_spec.rb'` — the README's own example.
RSpec.describe Order do
  # @intent: { "entity": "Order", "action": "total", "behavior": "sums line item prices after applying the active discount", "layer": "unit" }
  it "sums the line items" do
    expect(order.total).to eq(42)
  end
end
