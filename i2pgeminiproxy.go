package main

import (
	"bufio"
	"crypto/tls"
	"errors"
	"io"
	"log"
	"net"
	"net/url"
	"strings"
	"time"
)

func parseRequest(requestString string) (*url.URL, error) {
	//* Parse a gemini request as bytes into a destination URI

	if !strings.HasSuffix(requestString, "\r\n") {
		return nil, errors.New("request doesn't end in CR/LF")
	}

	// Parse the request URI, slicing off the last 2 bytes (the \r\n)
	reqUrl, errUrl := url.Parse(requestString[:len(requestString)-2])
	if errUrl != nil {
		return nil, errUrl
	}

	if reqUrl.Scheme != "gemini" {
		return nil, errors.New("incoming request has non-gemini scheme")
	}

	// TODO: more validation (length?)
	return reqUrl, nil
}

func makeOutgoing(requestUri *url.URL, tlsConfig *tls.Config) ([]byte, error) {
	//* Make an outgoing request to a destination gemini server with the given URI,
	//* and return the response as bytes

	// TODO (important): Verify certificates with TOFU
	// Also TODO, verify that incoming certificate actually matches the hostname

	destPort := requestUri.Port()
	if destPort == "" { // Set to default gemini port 1965 if none was given by the client
		destPort = "1965"
	}

	outgoingTlsConn, errConn := tls.Dial("tcp", requestUri.Hostname()+":"+destPort, tlsConfig)
	if errConn != nil {
		return nil, errConn
	}
	// 10 second timeout
	// TODO: make the timeout not hardcoded
	outgoingTlsConn.SetDeadline(time.Now().Add(time.Second * 10))

	outgoingTlsConn.Write([]byte(requestUri.String() + "\r\n"))

	// Read response
	responseBytes, errRead := io.ReadAll(outgoingTlsConn)
	if errRead != nil {
		return nil, errRead
	}

	return responseBytes, nil
}

func listenSingle(tlsConn net.Conn) (string, error) {
	//* Listen for a single incoming request and return it as a string

	// 10 second timeout
	// TODO: make the timeout not hardcoded
	tlsConn.SetDeadline(time.Now().Add(time.Second * 10))

	// Read request
	reader := bufio.NewReader(tlsConn)
	requestString, errRead := reader.ReadString('\n')
	if errRead != nil {
		return "", errRead
	}
	return requestString, nil
}

func redirectSingle(listener net.Listener, tlsConfig *tls.Config) error {
	//* Redirect a single incoming request to the destination

	log.Println("waiting for tls connection...")
	tlsConn, errAccept := listener.Accept()
	if errAccept != nil {
		return errAccept
	}
	defer tlsConn.Close()
	log.Println("got TLS connection")
	// TODO: respond with status 43 instead of closing upon error

	log.Println("listening for request...")
	requestString, errListen := listenSingle(tlsConn)
	if errListen != nil {
		log.Println("Error trying to listen for incoming tls connection")
		return errListen
	}

	log.Println("parsing request...")
	parsed, errParse := parseRequest(requestString)
	if errParse != nil {
		log.Println("Error parsing request from the client")
		return errParse
	}

	log.Println("making outgoing connection to remote server")
	responseBytes, errResponse := makeOutgoing(parsed, tlsConfig)
	if errResponse != nil {
		log.Println("Error making outgoing connection to destination server")
		return errResponse
	}
	tlsConn.SetDeadline(time.Now().Add(time.Second * 10))
	tlsConn.Write(responseBytes)

	return nil
}

func main() {
	cert, errCert := tls.LoadX509KeyPair("testdata/cert.pem", "testdata/key.pem")
	if errCert != nil {
		log.Fatal(errCert)
	}
	tlsConfig := &tls.Config{
		Certificates:       []tls.Certificate{cert},
		InsecureSkipVerify: true,
	}

	listener, errListen := tls.Listen("tcp", "127.0.0.1:1965", tlsConfig)
	if errListen != nil {
		log.Fatal(errListen)
	}
	defer listener.Close()
	log.Println("opened tls listener...")

	errRedirect := redirectSingle(listener, tlsConfig)
	if errRedirect != nil {
		log.Fatal(errRedirect)
	}
}
