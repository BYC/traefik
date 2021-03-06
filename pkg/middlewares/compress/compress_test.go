package compress

import (
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/NYTimes/gziphandler"
	"github.com/containous/traefik/pkg/testhelpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	acceptEncodingHeader  = "Accept-Encoding"
	contentEncodingHeader = "Content-Encoding"
	contentTypeHeader     = "Content-Type"
	varyHeader            = "Vary"
	gzipValue             = "gzip"
)

func TestShouldCompressWhenNoContentEncodingHeader(t *testing.T) {
	req := testhelpers.MustNewRequest(http.MethodGet, "http://localhost", nil)
	req.Header.Add(acceptEncodingHeader, gzipValue)

	baseBody := generateBytes(gziphandler.DefaultMinSize)

	next := http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		_, err := rw.Write(baseBody)
		assert.NoError(t, err)
	})
	handler := &compress{next: next}

	rw := httptest.NewRecorder()
	handler.ServeHTTP(rw, req)

	assert.Equal(t, gzipValue, rw.Header().Get(contentEncodingHeader))
	assert.Equal(t, acceptEncodingHeader, rw.Header().Get(varyHeader))

	if assert.ObjectsAreEqualValues(rw.Body.Bytes(), baseBody) {
		assert.Fail(t, "expected a compressed body", "got %v", rw.Body.Bytes())
	}
}

func TestShouldNotCompressWhenContentEncodingHeader(t *testing.T) {
	req := testhelpers.MustNewRequest(http.MethodGet, "http://localhost", nil)
	req.Header.Add(acceptEncodingHeader, gzipValue)

	fakeCompressedBody := generateBytes(gziphandler.DefaultMinSize)
	next := http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Add(contentEncodingHeader, gzipValue)
		rw.Header().Add(varyHeader, acceptEncodingHeader)
		_, err := rw.Write(fakeCompressedBody)
		if err != nil {
			http.Error(rw, err.Error(), http.StatusInternalServerError)
		}
	})
	handler := &compress{next: next}

	rw := httptest.NewRecorder()
	handler.ServeHTTP(rw, req)

	assert.Equal(t, gzipValue, rw.Header().Get(contentEncodingHeader))
	assert.Equal(t, acceptEncodingHeader, rw.Header().Get(varyHeader))

	assert.EqualValues(t, rw.Body.Bytes(), fakeCompressedBody)
}

func TestShouldNotCompressWhenNoAcceptEncodingHeader(t *testing.T) {
	req := testhelpers.MustNewRequest(http.MethodGet, "http://localhost", nil)

	fakeBody := generateBytes(gziphandler.DefaultMinSize)
	next := http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		_, err := rw.Write(fakeBody)
		if err != nil {
			http.Error(rw, err.Error(), http.StatusInternalServerError)
		}
	})
	handler := &compress{next: next}

	rw := httptest.NewRecorder()
	handler.ServeHTTP(rw, req)

	assert.Empty(t, rw.Header().Get(contentEncodingHeader))
	assert.EqualValues(t, rw.Body.Bytes(), fakeBody)
}

func TestShouldNotCompressWhenGRPC(t *testing.T) {
	req := testhelpers.MustNewRequest(http.MethodGet, "http://localhost", nil)
	req.Header.Add(acceptEncodingHeader, gzipValue)
	req.Header.Add(contentTypeHeader, "application/grpc")

	baseBody := generateBytes(gziphandler.DefaultMinSize)
	next := http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		_, err := rw.Write(baseBody)
		if err != nil {
			http.Error(rw, err.Error(), http.StatusInternalServerError)
		}
	})
	handler := &compress{next: next}

	rw := httptest.NewRecorder()
	handler.ServeHTTP(rw, req)

	assert.Empty(t, rw.Header().Get(acceptEncodingHeader))
	assert.Empty(t, rw.Header().Get(contentEncodingHeader))
	assert.EqualValues(t, rw.Body.Bytes(), baseBody)
}

func TestIntegrationShouldNotCompress(t *testing.T) {
	fakeCompressedBody := generateBytes(100000)

	testCases := []struct {
		name               string
		handler            http.Handler
		expectedStatusCode int
	}{
		{
			name: "when content already compressed",
			handler: http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
				rw.Header().Add(contentEncodingHeader, gzipValue)
				rw.Header().Add(varyHeader, acceptEncodingHeader)
				_, err := rw.Write(fakeCompressedBody)
				if err != nil {
					http.Error(rw, err.Error(), http.StatusInternalServerError)
				}
			}),
			expectedStatusCode: http.StatusOK,
		},
		{
			name: "when content already compressed and status code Created",
			handler: http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
				rw.Header().Add(contentEncodingHeader, gzipValue)
				rw.Header().Add(varyHeader, acceptEncodingHeader)
				rw.WriteHeader(http.StatusCreated)
				_, err := rw.Write(fakeCompressedBody)
				if err != nil {
					http.Error(rw, err.Error(), http.StatusInternalServerError)
				}
			}),
			expectedStatusCode: http.StatusCreated,
		},
	}

	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {
			compress := &compress{next: test.handler}
			ts := httptest.NewServer(compress)
			defer ts.Close()

			req := testhelpers.MustNewRequest(http.MethodGet, ts.URL, nil)
			req.Header.Add(acceptEncodingHeader, gzipValue)

			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)

			assert.Equal(t, test.expectedStatusCode, resp.StatusCode)

			assert.Equal(t, gzipValue, resp.Header.Get(contentEncodingHeader))
			assert.Equal(t, acceptEncodingHeader, resp.Header.Get(varyHeader))

			body, err := ioutil.ReadAll(resp.Body)
			require.NoError(t, err)
			assert.EqualValues(t, fakeCompressedBody, body)
		})
	}
}

func TestShouldWriteHeaderWhenFlush(t *testing.T) {
	next := http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Add(contentEncodingHeader, gzipValue)
		rw.Header().Add(varyHeader, acceptEncodingHeader)
		rw.WriteHeader(http.StatusUnauthorized)
		rw.(http.Flusher).Flush()
		_, err := rw.Write([]byte("short"))
		if err != nil {
			http.Error(rw, err.Error(), http.StatusInternalServerError)
		}
	})
	handler := &compress{next: next}
	ts := httptest.NewServer(handler)
	defer ts.Close()

	req := testhelpers.MustNewRequest(http.MethodGet, ts.URL, nil)
	req.Header.Add(acceptEncodingHeader, gzipValue)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)

	assert.Equal(t, gzipValue, resp.Header.Get(contentEncodingHeader))
	assert.Equal(t, acceptEncodingHeader, resp.Header.Get(varyHeader))
}

func TestIntegrationShouldCompress(t *testing.T) {
	fakeBody := generateBytes(100000)

	testCases := []struct {
		name               string
		handler            http.Handler
		expectedStatusCode int
	}{
		{
			name: "when AcceptEncoding header is present",
			handler: http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
				_, err := rw.Write(fakeBody)
				if err != nil {
					http.Error(rw, err.Error(), http.StatusInternalServerError)
				}
			}),
			expectedStatusCode: http.StatusOK,
		},
		{
			name: "when AcceptEncoding header is present and status code Created",
			handler: http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
				rw.WriteHeader(http.StatusCreated)
				_, err := rw.Write(fakeBody)
				if err != nil {
					http.Error(rw, err.Error(), http.StatusInternalServerError)
				}
			}),
			expectedStatusCode: http.StatusCreated,
		},
	}

	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {
			compress := &compress{next: test.handler}
			ts := httptest.NewServer(compress)
			defer ts.Close()

			req := testhelpers.MustNewRequest(http.MethodGet, ts.URL, nil)
			req.Header.Add(acceptEncodingHeader, gzipValue)

			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)

			assert.Equal(t, test.expectedStatusCode, resp.StatusCode)

			assert.Equal(t, gzipValue, resp.Header.Get(contentEncodingHeader))
			assert.Equal(t, acceptEncodingHeader, resp.Header.Get(varyHeader))

			body, err := ioutil.ReadAll(resp.Body)
			require.NoError(t, err)
			if assert.ObjectsAreEqualValues(body, fakeBody) {
				assert.Fail(t, "expected a compressed body", "got %v", body)
			}
		})
	}
}

func generateBytes(len int) []byte {
	var value []byte
	for i := 0; i < len; i++ {
		value = append(value, 0x61+byte(i))
	}
	return value
}
