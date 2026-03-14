package main

import (
	"bufio"
	"crypto/tls"
	"errors"
	"flag"
	"io"
	"log"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/eyedeekay/goSam"
)

const MAX_URI_LEN = 1024       // bytes
const REQUEST_TIMEOURT = 10000 // milliseconds
const RESPONSE_TIMEOUT = 10000 // milliseconds

func parseRequest(requestString string) (*url.URL, error) {
	//* Parse a gemini request as a string into a destination URI

	if len(requestString) > MAX_URI_LEN+len("\r\n") { // URI len + CRLF
		return nil, errors.New("request is too long")
	}
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

	return reqUrl, nil
}

func makeOutgoing(requestUri *url.URL, tlsConfig *tls.Config, sam *goSam.Client) ([]byte, error) {
	//* Make an outgoing request to a destination gemini server on i2p with the
	//* given URI, and return the response as bytes

	// TODO (important): Verify certificates with TOFU
	// Also TODO, verify that incoming certificate actually matches the hostname

	destPort := requestUri.Port()
	if destPort == "" { // Set to default gemini port 1965 if none was given by the client
		destPort = "1965"
	}

	outTlsConfig := tlsConfig.Clone()
	outTlsConfig.ServerName = requestUri.Hostname()

	i2pConn, errI2pConn := sam.Dial("tcp", requestUri.Hostname()+":"+destPort)
	if errI2pConn != nil {
		return nil, errI2pConn
	}
	i2pTcpTransport := tls.Client(i2pConn, outTlsConfig)

	// Add timeout
	i2pTcpTransport.SetDeadline(time.Now().Add(time.Millisecond * RESPONSE_TIMEOUT))

	i2pTcpTransport.Write([]byte(requestUri.String() + "\r\n"))

	// Read response
	responseBytes, errRead := io.ReadAll(i2pTcpTransport)
	if errRead != nil {
		return nil, errRead
	}

	return responseBytes, nil
}

func listenSingle(tlsConn net.Conn) (string, error) {
	//* Listen for a single incoming request and return it as a string

	// timeout
	tlsConn.SetDeadline(time.Now().Add(time.Millisecond * REQUEST_TIMEOURT))

	// Read request
	reader := bufio.NewReader(tlsConn)
	requestString, errRead := reader.ReadString('\n')
	if errRead != nil {
		return "", errRead
	}
	return requestString, nil
}

func redirectSingle(tlsConn net.Conn, tlsConfig *tls.Config, sam *goSam.Client) error {
	//* Redirect a single incoming request to the destination

	log.Println("listening for request...")
	requestString, errListen := listenSingle(tlsConn)
	if errListen != nil {
		log.Println("Error trying to listen for incoming tls connection")
		return errListen
	}

	// Set timeout
	tlsConn.SetDeadline(time.Now().Add(time.Millisecond * REQUEST_TIMEOURT))

	// Parse the uri in the given request into a url object
	log.Println("parsing uri in given request...")
	parsed, errParse := parseRequest(requestString)
	if errParse != nil {
		tlsConn.Write([]byte("43\r\n"))
		log.Println("Error parsing request from the client (43)")
		log.Println(errParse)
		return nil
	}
	if !strings.HasSuffix(strings.ToLower(parsed.Hostname()), ".i2p") {
		log.Println("Cannot accept request from the client (43)")
		log.Println("Requested uri is not an i2p address")
		return nil
	}

	log.Println("making outgoing connection to remote server")
	responseBytes, errResponse := makeOutgoing(parsed, tlsConfig, sam)
	if errResponse != nil {
		log.Println("Error making outgoing connection to destination server (43)")
		log.Println(errResponse)
		return nil
	}
	tlsConn.Write(responseBytes)

	return nil
}

func main() {
	listenFlag := flag.String(
		"l",
		"127.0.0.1:1965",
		"ip/port on which to listen for incoming gemini connections",
	)
	listenHost, listenPort, errParse := net.SplitHostPort(*listenFlag)
	if errParse != nil {
		log.Fatal(errParse)
	}
	cert, errCert := tls.LoadX509KeyPair("testdata/cert.pem", "testdata/key.pem")
	if errCert != nil {
		log.Fatal(errCert)
	}
	tlsConfig := &tls.Config{
		Certificates:       []tls.Certificate{cert},
		InsecureSkipVerify: true,
	}
	sam, errSam := goSam.NewDefaultClient()
	if errSam != nil {
		log.Fatal(errSam)
	}

	listener, errListen := tls.Listen("tcp", listenHost+":"+listenPort, tlsConfig)
	if errListen != nil {
		log.Fatal(errListen)
	}
	defer listener.Close()
	log.Println("opened tls listener...")

	for {
		// TODO: maybe handle errors in a better way?
		log.Println("waiting for tls connection...")
		tlsConn, errAccept := listener.Accept()
		if errAccept != nil {
			log.Println("Error trying to accept connection:")
			log.Println(errAccept)
		}
		log.Println("got TLS connection")

		go func() {
			defer tlsConn.Close()
			errRedirect := redirectSingle(tlsConn, tlsConfig, sam)
			if errRedirect != nil {
				log.Println("Error trying to redirect connection:")
				log.Println(errRedirect)
			}
		}()
	}
}
