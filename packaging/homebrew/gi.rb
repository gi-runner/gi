class Gi < Formula
  desc "One feature file for functional tests and load tests"
  homepage "https://github.com/gi-runner/gi"
  license "Apache-2.0"
  head "https://github.com/gi-runner/gi.git", branch: "main"
  depends_on "go" => :build

  def install
    system "go", "build", *std_go_args(ldflags: "-s -w"), "./cmd/gi"
  end

  test do
    assert_match "gi", shell_output("#{bin}/gi version")
  end
end
