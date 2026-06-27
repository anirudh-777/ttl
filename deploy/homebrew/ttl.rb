# Homebrew formula for ttl.
#
# To install locally without a tap:
#
#   brew install --build-from-source ttl.rb
#
# To publish in a tap, copy this file into your-tap/Formula/ttl.rb and
# update the url + sha256 fields.

class Ttl < Formula
  desc "Terminal-first, multi-tenant task tracker (CLI, TUI, web UI)"
  homepage "https://github.com/anirudhprakash/ttl"
  version "0.1.0"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/anirudhprakash/ttl/releases/download/v#{version}/ttl-darwin-arm64"
      sha256 "REPLACE_WITH_DARWIN_ARM64_SHA256"
    end
    on_intel do
      url "https://github.com/anirudhprakash/ttl/releases/download/v#{version}/ttl-darwin-amd64"
      sha256 "REPLACE_WITH_DARWIN_AMD64_SHA256"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/anirudhprakash/ttl/releases/download/v#{version}/ttl-linux-arm64"
      sha256 "REPLACE_WITH_LINUX_ARM64_SHA256"
    end
    on_intel do
      url "https://github.com/anirudhprakash/ttl/releases/download/v#{version}/ttl-linux-amd64"
      sha256 "REPLACE_WITH_LINUX_AMD64_SHA256"
    end
  end

  def install
    bin.install "ttl-#{OS}-#{Hardware::CPU.arch}" => "ttl"
  end

  test do
    system bin/"ttl", "version"
  end
end
