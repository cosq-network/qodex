class Qodex < Formula
  desc "Local-first coding agent for llama.cpp and Qwen Coder"
  homepage "https://github.com/benoybose/qodex"
  version "0.1.0"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/benoybose/qodex/releases/download/v#{version}/qodex_darwin_arm64.tar.gz"
      sha256 "0000000000000000000000000000000000000000000000000000000000000000"
    else
      url "https://github.com/benoybose/qodex/releases/download/v#{version}/qodex_darwin_x86_64.tar.gz"
      sha256 "0000000000000000000000000000000000000000000000000000000000000000"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/benoybose/qodex/releases/download/v#{version}/qodex_linux_arm64.tar.gz"
      sha256 "0000000000000000000000000000000000000000000000000000000000000000"
    else
      url "https://github.com/benoybose/qodex/releases/download/v#{version}/qodex_linux_x86_64.tar.gz"
      sha256 "0000000000000000000000000000000000000000000000000000000000000000"
    end
  end

  def install
    bin.install "qodex"
  end

  test do
    assert_match "qodex version #{version}", shell_output("#{bin}/qodex version")
  end
end
