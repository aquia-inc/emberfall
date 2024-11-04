# typed: false
# frozen_string_literal: true

# This file was generated by GoReleaser. DO NOT EDIT.
class Emberfall < Formula
  desc "Smoke testing for HTTP services made easy!"
  homepage ""
  version "0.2.0"

  on_macos do
    on_intel do
      url "https://github.com/aquia-inc/emberfall/releases/download/v0.2.0/emberfall_Darwin_x86_64.tar.gz", using: CurlDownloadStrategy,
        headers: [
          "Accept: application/octet-stream"
        ]
      sha256 "f3ac3f0174bfd2bff046480d2723dae53bb6ae69dbb34b68cd9c1dab0b486ffc"

      def install
        bin.install "emberfall"
      end
    end
    on_arm do
      url "https://github.com/aquia-inc/emberfall/releases/download/v0.2.0/emberfall_Darwin_arm64.tar.gz", using: CurlDownloadStrategy,
        headers: [
          "Accept: application/octet-stream"
        ]
      sha256 "01ced81ad82491c727ed9b223e056eec294f3b0c96bb8d651d43b86a75496f47"

      def install
        bin.install "emberfall"
      end
    end
  end

  on_linux do
    on_intel do
      if Hardware::CPU.is_64_bit?
        url "https://github.com/aquia-inc/emberfall/releases/download/v0.2.0/emberfall_Linux_x86_64.tar.gz", using: CurlDownloadStrategy,
          headers: [
            "Accept: application/octet-stream"
          ]
        sha256 "8cf68e7e151632a422010324cd179f2a9f9fd2f194a9f46f6b54910645dbbf5a"

        def install
          bin.install "emberfall"
        end
      end
    end
    on_arm do
      if Hardware::CPU.is_64_bit?
        url "https://github.com/aquia-inc/emberfall/releases/download/v0.2.0/emberfall_Linux_arm64.tar.gz", using: CurlDownloadStrategy,
          headers: [
            "Accept: application/octet-stream"
          ]
        sha256 "e66459dfbcc99c3238c20ff1251677b8fbd8462b712c2ff06cd0b8aad0e89c4c"

        def install
          bin.install "emberfall"
        end
      end
    end
  end
end
