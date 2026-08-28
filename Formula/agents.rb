# typed: false
# frozen_string_literal: true

class Agents < Formula
  desc "Development harness and standalone agent tool"
  homepage "https://github.com/nilbot/dotfiles"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/nilbot/dotfiles/releases/download/v0.1.0/agents_v0.1.0_darwin_arm64.tar.gz"
      sha256 "c8419260b3490b54bde0cac0c68fe716bbfaf0c89da6d90166ab291c5bb2f235"
    end
    on_intel do
      url "https://github.com/nilbot/dotfiles/releases/download/v0.1.0/agents_v0.1.0_darwin_amd64.tar.gz"
      sha256 "54fbc3177719ae13cd4907d65d3c0f4acc7d7ede5027f58ac9a3f06e2371a4ca"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/nilbot/dotfiles/releases/download/v0.1.0/agents_v0.1.0_linux_arm64.tar.gz"
      sha256 "38f7d8dbbd48283f14a3f06c9690f43bfe76631453dae3b247557f07fd439791"
    end
    on_intel do
      url "https://github.com/nilbot/dotfiles/releases/download/v0.1.0/agents_v0.1.0_linux_amd64.tar.gz"
      sha256 "777dab67729c5acf1902aa1f67d111a0e5f3b8a76d6d671f0ce9426e256c88fe"
    end
  end

  def install
    bin.install "agents"
  end

  test do
    assert_match "agents", shell_output("#{bin}/agents version")
  end
end
