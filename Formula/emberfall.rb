# typed: false
# frozen_string_literal: true

# This file was generated by GoReleaser. DO NOT EDIT.
class Emberfall < Formula
  desc "Smoke testing for HTTP services made easy!"
  homepage ""
  version "0.1.0"

  on_macos do
    on_intel do
      url "https://github.com/aquia-inc/emberfall/releases/download/v0.1.0/emberfall_Darwin_x86_64.tar.gz", using: CurlDownloadStrategy,
        headers: [
          "Accept: application/octet-stream"
        ]
      sha256 "0edd4130a3bb23f1f565bcfad0bb9c957fd4e030b01ae52c00e89b1e0a266bb2"

      def install
        bin.install "emberfall"
      end
    end
    on_arm do
      url "https://github.com/aquia-inc/emberfall/releases/download/v0.1.0/emberfall_Darwin_arm64.tar.gz", using: CurlDownloadStrategy,
        headers: [
          "Accept: application/octet-stream"
        ]
      sha256 "be36dbf2d85f63e6dd9555c12430b3124fbc9faa7d872a4f21fdfa7f537891e8"

      def install
        bin.install "emberfall"
      end
    end
  end

  on_linux do
    on_intel do
      if Hardware::CPU.is_64_bit?
        url "https://github.com/aquia-inc/emberfall/releases/download/v0.1.0/emberfall_Linux_x86_64.tar.gz", using: CurlDownloadStrategy,
          headers: [
            "Accept: application/octet-stream"
          ]
        sha256 "30a48b2b419ec64ab8629088eddad8db35d3ad7ebe650edf440f520a309df53e"

        def install
          bin.install "emberfall"
        end
      end
    end
    on_arm do
      if Hardware::CPU.is_64_bit?
        url "https://github.com/aquia-inc/emberfall/releases/download/v0.1.0/emberfall_Linux_arm64.tar.gz", using: CurlDownloadStrategy,
          headers: [
            "Accept: application/octet-stream"
          ]
        sha256 "1d7b70216219e9744d77ab7faee7ca4568bdf2671d5627ff21b1a91e044976a2"

        def install
          bin.install "emberfall"
        end
      end
    end
  end
end