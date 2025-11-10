#!/bin/bash
set -ex

# Generate CA's key
openssl genrsa -aes256 -passout pass:1 -out ca.key.pem 4096

# Remove password protection for ease of use
openssl rsa -passin pass:1 -in ca.key.pem -out ca.key.pem.tmp
mv ca.key.pem.tmp ca.key.pem

# Generate CA certificate
openssl req -config openssl.cnf -key ca.key.pem -new -x509 -days 7300 -sha256 -extensions v3_ca -out ca.pem

echo "CA certificate generated successfully!"
echo "CA certificate: ca.pem"
echo "CA private key: ca.key.pem"
echo ""
echo "To install the CA certificate:"
echo "  - Browser: Import ca.pem as a trusted root certificate"
echo "  - Linux: sudo cp ca.pem /usr/local/share/ca-certificates/network-proxy-ca.crt && sudo update-ca-certificates"
echo "  - macOS: sudo security add-trusted-cert -d -r trustRoot -k /Library/Keychains/System.keychain ca.pem"
