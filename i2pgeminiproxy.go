package main

import (
	"bufio"
	"crypto/tls"
	"errors"
	"flag"
	"github.com/eyedeekay/goSam"
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

	// 10 second timeout
	// TODO: make the timeout not hardcoded
	i2pTcpTransport.SetDeadline(time.Now().Add(time.Second * 10))

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

func redirectSingle(tlsConn net.Conn, tlsConfig *tls.Config, sam *goSam.Client) error {
	//* Redirect a single incoming request to the destination

	// TODO: respond with status 43 instead of closing upon error

	log.Println("listening for request...")
	requestString, errListen := listenSingle(tlsConn)
	if errListen != nil {
		log.Println("Error trying to listen for incoming tls connection")
		return errListen
	}

	// Parse the uri in the given request into a url object
	log.Println("parsing uri in given request...")
	parsed, errParse := parseRequest(requestString)
	if errParse != nil {
		log.Println("Error parsing request from the client")
		return errParse
	}
	if !strings.HasSuffix(strings.ToLower(parsed.Hostname()), ".i2p") {
		log.Println("Requested uri is not an i2p address")
		return errors.New("Requested uri is not an i2p address")
	}

	log.Println("making outgoing connection to remote server")
	responseBytes, errResponse := makeOutgoing(parsed, tlsConfig, sam)
	if errResponse != nil {
		log.Println("Error making outgoing connection to destination server")
		return errResponse
	}
	tlsConn.SetDeadline(time.Now().Add(time.Second * 10))
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
