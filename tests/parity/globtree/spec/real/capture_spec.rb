# Reached twice by `--source 'spec/**/*_spec.rb'`: by its real path, and again
# through the spec/linked symlink. A port that does not follow symlinks reports
# this file once instead of twice.
RSpec.describe Payment do
  # @intent: { entity: 'Payment', action: 'capture', behavior: 'captures the authorized amount once the order ships', layer: 'unit' }
  it "captures on ship" do
    expect(payment.capture!).to be(true)
  end

  # A second annotation, so the per-file finding ORDER is compared too.
  # @intent: {entity:"Payment",action:"capture",behavior:"is idempotent when the capture is retried",layer:"unit"}
  it "is idempotent" do
    expect { payment.capture! }.not_to change { payment.reload.captured_at }
  end
end
