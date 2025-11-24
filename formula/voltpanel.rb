class Voltpanel < Formula
  desc "Alresia VoltPanel local server management daemon"
  homepage "https://alresia.com"
  version "0.1.0"
  url "https://github.com/alresia/voltpanel/releases/download/v0.1.0/voltpanel_Darwin_arm64.tar.gz"
  sha256 "REPLACE_ME"

  def install
    bin.install "voltpanel"
  end

  test do
    system "#{bin}/voltpanel", "-h"
  end
end
